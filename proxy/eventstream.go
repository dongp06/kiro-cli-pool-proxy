package proxy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// AWS EventStream framing constants (confirmed from kiro-cli / Kiro-Go 1:1).
const (
	esPreludeLen    = 12 // total_len(4) + headers_len(4) + prelude_crc(4)
	esMsgCRCLen     = 4  // trailing message CRC
	esMinMsgBytes   = esPreludeLen + esMsgCRCLen
	esMaxMsgBytes   = 25 * 1024 * 1024 // 25 MiB sanity cap
	esMaxHeaderLen  = 128 * 1024       // 128 KiB header cap
	esMaxBufferSize = 32 * 1024 * 1024 // stop parsing beyond this (safety)
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

	// Credits is the turn's cumulative credit (sum of all meteringEvents).
	Credits     float64
	SawMetering bool
	// ContextPct is the last observed context-window usage percentage.
	ContextPct float64
	SawContext bool
	// Frames counts total decoded frames (diagnostics).
	Frames int
	// Types records how many frames of each :event-type were seen (RE/debug).
	Types map[string]int
	// Token usage is reported by Kiro in metadata/usage payloads. Keep separate
	// presence flags so a real zero is distinguishable from missing telemetry.
	Tokens TokenMeter
}

// TokenMeter holds token counts reported by Kiro itself. Counts are
// cumulative snapshots, so the last reported value for each side wins.
type TokenMeter struct {
	InputTokens     int
	OutputTokens    int
	SawInputTokens  bool
	SawOutputTokens bool
}

func tokenCountPtr(value int, seen bool) *int {
	if !seen {
		return nil
	}
	return &value
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
	eventType = canonicalEventType(eventType)
	m.Tokens.Observe(payload)
	if eventType != "" {
		if m.Types == nil {
			m.Types = map[string]int{}
		}
		m.Types[eventType]++
	}
	switch eventType {
	case "meteringEvent":
		if usage, ok := creditFromPayload(payload); ok {
			m.Credits += usage
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

// Observe extracts upstream-reported token counts from one Kiro event payload.
// It intentionally never estimates from text or context percentage.
func (m *TokenMeter) Observe(payload []byte) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var event map[string]any
	if decoder.Decode(&event) != nil {
		return
	}

	candidates := []map[string]any{event}
	collectTokenUsageMaps(event, &candidates)
	for _, usage := range candidates {
		if output, ok := readTokenCount(usage,
			"outputTokens", "completionTokens", "totalOutputTokens",
			"output_tokens", "completion_tokens", "total_output_tokens",
		); ok {
			m.OutputTokens = output
			m.SawOutputTokens = true
		}

		if input, ok := readTokenCount(usage,
			"inputTokens", "promptTokens", "totalInputTokens",
			"input_tokens", "prompt_tokens", "total_input_tokens",
		); ok {
			m.InputTokens = input
			m.SawInputTokens = true
			continue
		}

		uncached, sawUncached := readTokenCount(usage, "uncachedInputTokens", "uncached_input_tokens")
		cacheRead, sawCacheRead := readTokenCount(usage, "cacheReadInputTokens", "cache_read_input_tokens")
		cacheWrite, sawCacheWrite := readTokenCount(usage,
			"cacheWriteInputTokens", "cache_write_input_tokens",
			"cacheCreationInputTokens", "cache_creation_input_tokens",
		)
		if sawUncached || sawCacheRead || sawCacheWrite {
			m.InputTokens = uncached + cacheRead + cacheWrite
			m.SawInputTokens = true
			continue
		}

		if total, ok := readTokenCount(usage, "totalTokens", "total_tokens"); ok && m.SawOutputTokens && total >= m.OutputTokens {
			m.InputTokens = total - m.OutputTokens
			m.SawInputTokens = true
		}
	}
}

// collectTokenUsageMaps visits known usage containers deterministically. This
// avoids map iteration order deciding which cumulative snapshot wins.
func collectTokenUsageMaps(value any, out *[]map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := v[key]
			norm := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if norm == "usage" || norm == "tokenusage" {
				if usage, ok := child.(map[string]any); ok {
					*out = append(*out, usage)
				}
			}
			collectTokenUsageMaps(child, out)
		}
	case []any:
		for _, child := range v {
			collectTokenUsageMaps(child, out)
		}
	}
}

func readTokenCount(value map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok {
			continue
		}
		var count int64
		var err error
		switch n := raw.(type) {
		case json.Number:
			count, err = n.Int64()
			if err != nil {
				var f float64
				f, err = n.Float64()
				count = int64(f)
			}
		case float64:
			count = int64(n)
		case int:
			count = int64(n)
		case int64:
			count = n
		case string:
			count, err = strconv.ParseInt(strings.TrimSpace(n), 10, 64)
			if err != nil {
				var f float64
				f, err = strconv.ParseFloat(strings.TrimSpace(n), 64)
				count = int64(f)
			}
		default:
			continue
		}
		if err == nil && count >= 0 {
			return int(count), true
		}
	}
	return 0, false
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

// canonicalEventType keeps parsing resilient to runtimes that serialize the
// Smithy union name as kebab-case or snake_case instead of camelCase.
func canonicalEventType(eventType string) string {
	n := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.TrimSpace(eventType)))
	switch n {
	case "meteringevent":
		return "meteringEvent"
	case "contextusageevent":
		return "contextUsageEvent"
	case "assistantresponseevent":
		return "assistantResponseEvent"
	case "reasoningcontentevent":
		return "reasoningContentEvent"
	case "tooluseevent":
		return "toolUseEvent"
	case "metadataevent":
		return "metadataEvent"
	}
	return eventType
}

// creditFromPayload extracts the credit usage from a meteringEvent payload.
// Handles flat {"usage": N} and double-wrapped {"meteringEvent": {"usage": N}}.
func creditFromPayload(payload []byte) (float64, bool) {
	var value any
	if json.Unmarshal(payload, &value) != nil {
		return 0, false
	}
	return findCredit(value)
}

// findCredit tolerates wrappers and numeric strings, but deliberately accepts
// only Kiro's metering field named "usage". Aliases such as credit/credits are
// not part of the observed wire contract and could misattribute other values.
// A present zero is valid and is still reported as found.
func findCredit(value any) (float64, bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			norm := strings.ToLower(strings.TrimSpace(key))
			if norm == "usage" {
				if n, ok := numericValue(child); ok {
					return n, true
				}
			}
		}
		for _, child := range v {
			if n, ok := findCredit(child); ok {
				return n, true
			}
		}
	case []any:
		for _, child := range v {
			if n, ok := findCredit(child); ok {
				return n, true
			}
		}
	}
	return 0, false
}

func numericValue(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
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
