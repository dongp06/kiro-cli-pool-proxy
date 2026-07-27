package proxy

import (
	"encoding/json"
	"sync"
)

// Event is a single server-sent event delivered to admin-panel subscribers.
// Type discriminates the payload: "log" (a new LogEntry), "quota" (an account's
// updated quota snapshot), or "accounts" (the account list changed).
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// EventHub is a fan-out broadcaster for admin-panel SSE. Publishers call
// Publish; each subscriber owns a buffered channel and is dropped if it can't
// keep up (slow client), so a stalled browser never blocks the proxy hot path.
type EventHub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// NewEventHub creates an empty hub.
func NewEventHub() *EventHub {
	return &EventHub{subs: make(map[chan Event]struct{})}
}

// Subscribe registers a new subscriber and returns its channel plus an
// unsubscribe function the caller must invoke when done.
func (h *EventHub) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish delivers an event to all subscribers. Non-blocking: a subscriber
// whose buffer is full is skipped for this event rather than blocking others.
func (h *EventHub) Publish(e Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			// Slow subscriber; drop this event for them.
		}
	}
}

// encodeEvent renders an event as a `data: <json>\n\n` SSE frame.
func encodeEvent(e Event) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(b)+8)
	out = append(out, "data: "...)
	out = append(out, b...)
	out = append(out, '\n', '\n')
	return out, nil
}
