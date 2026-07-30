package proxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// buildFrame constructs a valid AWS EventStream frame with a single string
// header ":event-type" = eventType and the given JSON payload.
func buildFrame(eventType string, payload []byte) []byte {
	// Header: name_len(1) name(":event-type") value_type(7) value_len(2) value
	name := ":event-type"
	var hb bytes.Buffer
	hb.WriteByte(byte(len(name)))
	hb.WriteString(name)
	hb.WriteByte(7) // string type
	binary.Write(&hb, binary.BigEndian, uint16(len(eventType)))
	hb.WriteString(eventType)
	headers := hb.Bytes()

	totalLen := uint32(esPreludeLen + len(headers) + len(payload) + esMsgCRCLen)

	var f bytes.Buffer
	binary.Write(&f, binary.BigEndian, totalLen)
	binary.Write(&f, binary.BigEndian, uint32(len(headers)))
	binary.Write(&f, binary.BigEndian, uint32(0)) // prelude CRC (not validated by sink)
	f.Write(headers)
	f.Write(payload)
	binary.Write(&f, binary.BigEndian, uint32(0)) // message CRC (not validated)
	return f.Bytes()
}

func TestMeteringSinkExtractsCredit(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("assistantResponseEvent", []byte(`{"content":"hello"}`)))
	stream.Write(buildFrame("meteringEvent", []byte(`{"unit":"credit","usage":0.5}`)))
	stream.Write(buildFrame("meteringEvent", []byte(`{"unit":"credit","usage":1.25}`)))

	sink := &MeteringSink{}
	io.Copy(sink, &stream)

	if !sink.SawMetering {
		t.Fatal("expected SawMetering=true")
	}
	if sink.Credits != 1.75 {
		t.Fatalf("expected summed credit 1.75, got %v", sink.Credits)
	}
	if sink.Frames != 3 {
		t.Fatalf("expected 3 frames, got %d", sink.Frames)
	}
}

func TestMeteringSinkContextUsage(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("contextUsageEvent", []byte(`{"contextUsagePercentage":42.5}`)))

	sink := &MeteringSink{}
	io.Copy(sink, &stream)

	if !sink.SawContext || sink.ContextPct != 42.5 {
		t.Fatalf("expected context 42.5, got saw=%v pct=%v", sink.SawContext, sink.ContextPct)
	}
}

func TestMeteringSinkDoubleWrapped(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("meteringEvent", []byte(`{"meteringEvent":{"usage":2.0}}`)))

	sink := &MeteringSink{}
	io.Copy(sink, &stream)

	if !sink.SawMetering || sink.Credits != 2.0 {
		t.Fatalf("expected double-wrapped credit 2.0, got %v", sink.Credits)
	}
}

func TestMeteringSinkSumsAllMeteringEvents(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("meteringEvent", []byte(`{"usage":1.25}`)))
	stream.Write(buildFrame("meteringEvent", []byte(`{"usage":0.75}`)))
	stream.Write(buildFrame("meteringEvent", []byte(`{"usage":0}`)))

	sink := &MeteringSink{}
	_, _ = io.Copy(sink, &stream)
	if !sink.SawMetering || sink.Credits != 2 {
		t.Fatalf("expected summed credit 2, got saw=%v credit=%v", sink.SawMetering, sink.Credits)
	}
}

func TestMeteringSinkWrappedAndStringCredit(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("meteringEvent", []byte(`{"event":{"meteringEvent":{"usage":"4.75"}}}`)))
	sink := &MeteringSink{}
	io.Copy(sink, &stream)
	if !sink.SawMetering || sink.Credits != 4.75 {
		t.Fatalf("expected wrapped string credit 4.75, got saw=%v credit=%v", sink.SawMetering, sink.Credits)
	}
}

func TestCreditFromPayloadCreditAlias(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want float64
	}{
		{"credits", `{"result":{"credits":6.25}}`, 6.25},
		{"zero", `{"meteringEvent":{"usage":0}}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := creditFromPayload([]byte(tc.body))
			if !ok || got != tc.want {
				t.Fatalf("creditFromPayload() = (%v,%v), want (%v,true)", got, ok, tc.want)
			}
		})
	}
}

func TestCanonicalEventType(t *testing.T) {
	for input, want := range map[string]string{
		"metering-event":     "meteringEvent",
		"metering_event":     "meteringEvent",
		"context-usageEvent": "contextUsageEvent",
	} {
		if got := canonicalEventType(input); got != want {
			t.Errorf("canonicalEventType(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestMeteringSinkByteForByteSplit verifies parsing survives arbitrary chunk
// boundaries (frames split across multiple Write calls) — the tee scenario.
func TestMeteringSinkByteForByteSplit(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("assistantResponseEvent", []byte(`{"content":"hi"}`)))
	stream.Write(buildFrame("meteringEvent", []byte(`{"usage":3.5}`)))
	full := stream.Bytes()

	sink := &MeteringSink{}
	// Feed one byte at a time.
	for _, b := range full {
		sink.Write([]byte{b})
	}

	if !sink.SawMetering || sink.Credits != 3.5 {
		t.Fatalf("expected credit 3.5 across byte-split, got %v", sink.Credits)
	}
}

// TestMeteringSinkTeePreservesBytes verifies io.TeeReader passthrough yields the
// exact original bytes while the sink parses — the core proxy guarantee.
func TestMeteringSinkTeePreservesBytes(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(buildFrame("assistantResponseEvent", []byte(`{"content":"data"}`)))
	stream.Write(buildFrame("meteringEvent", []byte(`{"usage":0.9}`)))
	original := stream.Bytes()

	sink := &MeteringSink{}
	tee := io.TeeReader(bytes.NewReader(original), sink)

	var clientReceived bytes.Buffer
	io.Copy(&clientReceived, tee)

	if !bytes.Equal(clientReceived.Bytes(), original) {
		t.Fatal("client bytes differ from original — passthrough broken!")
	}
	if sink.Credits != 0.9 {
		t.Fatalf("expected credit 0.9, got %v", sink.Credits)
	}
}

// TestMeteringSinkCorruptStops verifies corrupt data stops accounting safely
// without panic or error.
func TestMeteringSinkCorruptStops(t *testing.T) {
	sink := &MeteringSink{}
	// Garbage that claims a huge total length.
	bad := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0x00, 0x00, 0, 0, 0, 0}
	n, err := sink.Write(bad)
	if err != nil || n != len(bad) {
		t.Fatalf("Write must always accept all bytes: n=%d err=%v", n, err)
	}
	if !sink.stopped {
		t.Fatal("expected parser to stop on corrupt length")
	}
}
