package proxy

import "sync"

// LogEntry is a single proxied-request record shown in the admin Logs tab.
type LogEntry struct {
	TimeUnix int64   `json:"timeUnix"`
	Account  string  `json:"account"`
	ApiKey   string  `json:"apiKey"`
	Status   int     `json:"status"`
	Credits  float64 `json:"credits"`
	Kind     string  `json:"kind"`
	Err      string  `json:"err,omitempty"`
}

// LogStore is an in-memory ring buffer of recent request logs. Entries are lost
// on restart by design — the proxy stays lightweight and avoids per-request I/O.
type LogStore struct {
	mu  sync.Mutex
	buf []LogEntry
	cap int
	hub *EventHub // optional; broadcasts each new entry to SSE subscribers
}

// NewLogStore creates a log store holding at most cap entries.
func NewLogStore(cap int) *LogStore {
	if cap <= 0 {
		cap = 500
	}
	return &LogStore{cap: cap, buf: make([]LogEntry, 0, cap)}
}

// SetHub attaches an event hub so new entries are pushed to SSE subscribers.
func (s *LogStore) SetHub(h *EventHub) {
	s.mu.Lock()
	s.hub = h
	s.mu.Unlock()
}

// Add appends an entry, dropping the oldest when the buffer is full.
func (s *LogStore) Add(e LogEntry) {
	s.mu.Lock()
	s.buf = append(s.buf, e)
	if len(s.buf) > s.cap {
		s.buf = s.buf[len(s.buf)-s.cap:]
	}
	hub := s.hub
	s.mu.Unlock()
	if hub != nil {
		hub.Publish(Event{Type: "log", Data: e})
	}
}

// Snapshot returns a reverse-chronological (newest first) copy of the entries.
func (s *LogStore) Snapshot() []LogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LogEntry, len(s.buf))
	for i, e := range s.buf {
		out[len(s.buf)-1-i] = e
	}
	return out
}

// Clear removes all entries.
func (s *LogStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = s.buf[:0]
}
