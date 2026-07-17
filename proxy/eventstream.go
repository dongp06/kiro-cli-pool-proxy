package proxy

import (
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"
)

// AWS EventStream framing constants (confirmed from kiro-cli / Kiro-Go 1:1).
const (
	esPreludeLen    = 12                // total_len(4) + headers_len(4) + prelude_crc(4)
	esMsgCRCLen     = 4                 // trailing message CRC
	esMinMsgBytes   = esPreludeLen + esMsgCRCLen
	esMaxMsgBytes   = 25 * 1024 * 1024  // 25 MiB sanity cap
	esMaxHeaderLen  = 128 * 1024        // 128 KiB header cap
	esMaxBufferSize = 32 * 1024 * 1024  // stop parsing beyond this (safety)
)

// MeteringSink is an io.Writer that incrementally parses an AWS EventStream as
// bytes flow through, extracting the turn's credit usage from meteringEvent
// frames WITHOUT interfering with the byte passthrough.
//
// Usage: wrap the upstream response body with io.TeeReader(body, sink). The CLI
// receives the exact bytes; this sink parses a duplicate copy for accounting.
//
// It is deliberately best-effort: any parse error simply stops accounting. It
// NEVER returns an error from Write (that would break the tee) and NEVER blocks.
type MeteringSink struct {
	buf     []byte
	stopped bool

	// Credits is the turn's cumulative credit (last-wins across meteringEvents).
	Credits     float64
	SawMetering bool
	// ContextPct is the last observed context-window usage percentage.
	ContextPct float64
	SawContext bool
	// Frames counts total decoded frames (diagnostics).
	Frames int
	// Types records how many frames of each :event-type were seen (RE/debug).
	Types map[string]int
}

// Write consumes bytes and parses any complete frames. Always returns len(p), nil.
func (m *MeteringSink) Write(p []byte) (int, error) {
	if m.stopped {
		return len(p), nil
	}
	m.buf = append(m.buf, p...)
	if len(m.buf) > esMaxBufferSize {
		// Something is off (not event-stream, or corrupt). Stop accounting.
		m.stopped = true
		m.buf = nil
		return len(p), nil
	}
	m.parseFrames()
	return len(p), nil
}

// parseFrames extracts every complete frame currently buffered.
func (m *MeteringSink) parseFrames() {
	for {
		if len(m.buf) < esPreludeLen {
			return // need more bytes for prelude
		}
		totalLen := binary.BigEndian.Uint32(m.buf[0:4])
		headersLen := binary.BigEndian.Uint32(m.buf[4:8])

		// Validate lengths. On anything suspicious, stop accounting safely.
		if totalLen < esMinMsgBytes || totalLen > esMaxMsgBytes ||
			headersLen > esMaxHeaderLen || uint32(headersLen)+esMinMsgBytes > totalLen {
			m.stopped = true
			m.buf = nil
			return
		}

		if uint32(len(m.buf)) < totalLen {
			return // wait for the rest of this frame
		}

		headersStart := uint32(esPreludeLen)
		headersEnd := headersStart + headersLen
		payloadEnd := totalLen - esMsgCRCLen

		headers := m.buf[headersStart:headersEnd]
		payload := m.buf[headersEnd:payloadEnd]

		m.Frames++
		m.handleFrame(headers, payload)

		// Advance past this frame.
		m.buf = m.buf[totalLen:]
	}
}

// handleFrame inspects a single frame's headers and payload.
func (m *MeteringSink) handleFrame(headers, payload []byte) {
	eventType := eventTypeFromHeaders(headers)
	if eventType != "" {
		if m.Types == nil {
			m.Types = map[string]int{}
		}
		m.Types[eventType]++
	}
	switch eventType {
	case "meteringEvent":
		if usage, ok := creditFromPayload(payload); ok {
			m.Credits = usage // last-wins (cumulative)
			m.SawMetering = true
		}
	case "contextUsageEvent":
		var obj map[string]interface{}
		if json.Unmarshal(payload, &obj) == nil {
			if pct, ok := numberField(obj, "contextUsagePercentage"); ok {
				m.ContextPct = pct
				m.SawContext = true
			}
		}
	}
}

// eventTypeFromHeaders decodes AWS EventStream headers and returns :event-type.
// Format per header: name_len(1) name(n) value_type(1) value...
func eventTypeFromHeaders(headers []byte) string {
	offset := 0
	for offset < len(headers) {
		nameLen := int(headers[offset])
		offset++
		if nameLen == 0 || nameLen > len(headers)-offset {
			return ""
		}
		name := string(headers[offset : offset+nameLen])
		offset += nameLen
		if offset >= len(headers) {
			return ""
		}
		valueType := headers[offset]
		offset++

		switch valueType {
		case 7: // String
			if len(headers)-offset < 2 {
				return ""
			}
			vlen := int(headers[offset])<<8 | int(headers[offset+1])
			offset += 2
			if vlen > len(headers)-offset {
				return ""
			}
			value := string(headers[offset : offset+vlen])
			offset += vlen
			if name == ":event-type" {
				return value
			}
		case 6: // Byte array
			if len(headers)-offset < 2 {
				return ""
			}
			l := int(headers[offset])<<8 | int(headers[offset+1])
			offset += 2 + l
		case 0, 1: // bool true/false, no value bytes
		case 2:
			offset += 1
		case 3:
			offset += 2
		case 4:
			offset += 4
		case 5, 8:
			offset += 8
		case 9: // UUID
			offset += 16
		default:
			return "" // unknown type, cannot continue safely
		}
	}
	return ""
}

// creditFromPayload extracts the credit usage from a meteringEvent payload.
// Handles flat {"usage": N} and double-wrapped {"meteringEvent": {"usage": N}}.
func creditFromPayload(payload []byte) (float64, bool) {
	var obj map[string]interface{}
	if json.Unmarshal(payload, &obj) != nil {
		return 0, false
	}
	if usage, ok := numberField(obj, "usage"); ok {
		return usage, true
	}
	if nested, ok := obj["meteringEvent"].(map[string]interface{}); ok {
		if usage, ok := numberField(nested, "usage"); ok {
			return usage, true
		}
	}
	return 0, false
}

// numberField reads a numeric field that may be float64, json.Number, or string.
func numberField(m map[string]interface{}, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
