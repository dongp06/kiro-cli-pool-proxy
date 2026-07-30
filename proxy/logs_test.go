package proxy

import (
	"bytes"
	"io"
	"kiro-cli-pool-proxy/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordChatUsage is the shared success path for the Anthropic (/v1/messages)
// and OpenAI (/v1/chat/completions) handlers. It must add a Logs entry so real
// client traffic (Claude Code, Codex, opencode) shows up on the admin Logs page,
// not just native Kiro-CLI traffic.
func TestRecordChatUsageAddsLogEntry(t *testing.T) {
	cfg := &config.Config{
		ApiKeys:  []config.APIKey{{ID: "key1", Name: "my-key", Key: "secret", Enabled: true}},
		Accounts: []config.Account{{ID: "acc1", Email: "user@example.com"}},
	}
	s := &Server{
		cfg:  cfg,
		logs: NewLogStore(10),
		hub:  NewEventHub(),
	}
	acc, _ := cfg.GetAccountByID("acc1")

	s.recordChatUsage(&acc, "key1", 1.5, 42, true)

	logs := s.logs.Snapshot()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry after recordChatUsage, got %d", len(logs))
	}
	e := logs[0]
	if e.Status != 200 {
		t.Errorf("status = %d, want 200", e.Status)
	}
	if e.Account != "user@example.com" {
		t.Errorf("account = %q, want email", e.Account)
	}
	if e.ApiKey != "my-key" {
		t.Errorf("apiKey = %q, want key name %q", e.ApiKey, "my-key")
	}
	if e.Credits != 1.5 {
		t.Errorf("credits = %v, want 1.5", e.Credits)
	}
	if !e.Metered {
		t.Error("metered = false, want true")
	}
	if e.Kind != "chat" {
		t.Errorf("kind = %q, want chat", e.Kind)
	}
}

// When the account has no email, the log should fall back to the account ID
// (matching the native Kiro path in server.go).
func TestRecordChatUsageFallsBackToAccountID(t *testing.T) {
	cfg := &config.Config{
		ApiKeys:  []config.APIKey{{ID: "key1", Key: "secret", Enabled: true}},
		Accounts: []config.Account{{ID: "acc-no-email"}},
	}
	s := &Server{cfg: cfg, logs: NewLogStore(10), hub: NewEventHub()}
	acc, _ := cfg.GetAccountByID("acc-no-email")

	s.recordChatUsage(&acc, "key1", 0, 0, false)

	logs := s.logs.Snapshot()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if logs[0].Account != "acc-no-email" {
		t.Errorf("account = %q, want fallback to ID", logs[0].Account)
	}
	// No key name set → apiKey label falls back to the key ID.
	if logs[0].ApiKey != "key1" {
		t.Errorf("apiKey = %q, want fallback to key ID", logs[0].ApiKey)
	}
}

// Every translated response path must add all per-frame metering charges. A
// trailing zero event is deliberately included: it used to overwrite the
// earlier charges and made the admin log show 0 credits.
func TestTranslatedHandlersSumMeteringEvents(t *testing.T) {
	stream := bytes.Join([][]byte{
		buildFrame("assistantResponseEvent", []byte(`{"content":"ok"}`)),
		buildFrame("meteringEvent", []byte(`{"usage":1.25}`)),
		buildFrame("meteringEvent", []byte(`{"usage":0.75}`)),
		buildFrame("meteringEvent", []byte(`{"usage":0}`)),
	}, nil)

	tests := []struct {
		name string
		run  func(*Server, http.ResponseWriter, *http.Response, *config.Account)
	}{
		{
			name: "anthropic_stream",
			run: func(s *Server, w http.ResponseWriter, resp *http.Response, account *config.Account) {
				s.streamAnthropic(w, resp, &anthRequest{Model: "test"}, account, "key1")
			},
		},
		{
			name: "anthropic_aggregate",
			run: func(s *Server, w http.ResponseWriter, resp *http.Response, account *config.Account) {
				s.aggregateAnthropic(w, resp, &anthRequest{Model: "test"}, account, "key1")
			},
		},
		{
			name: "openai_stream",
			run: func(s *Server, w http.ResponseWriter, resp *http.Response, account *config.Account) {
				s.streamOpenAI(w, resp, &oaiRequest{Model: "test"}, account, "key1")
			},
		},
		{
			name: "openai_aggregate",
			run: func(s *Server, w http.ResponseWriter, resp *http.Response, account *config.Account) {
				s.aggregateOpenAI(w, resp, &oaiRequest{Model: "test"}, account, "key1")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				ApiKeys:  []config.APIKey{{ID: "key1", Key: "secret", Enabled: true}},
				Accounts: []config.Account{{ID: "acc1", Email: "user@example.com"}},
			}
			s := &Server{cfg: cfg, logs: NewLogStore(10), hub: NewEventHub()}
			account, _ := cfg.GetAccountByID("acc1")
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(stream))}

			tc.run(s, httptest.NewRecorder(), resp, &account)

			logs := s.logs.Snapshot()
			if len(logs) != 1 {
				t.Fatalf("got %d log entries, want 1", len(logs))
			}
			if logs[0].Credits != 2 || !logs[0].Metered {
				t.Fatalf("log credits/metered = %v/%v, want 2/true", logs[0].Credits, logs[0].Metered)
			}
			updated, _ := cfg.GetAccountByID("acc1")
			if updated.Credits != 2 {
				t.Fatalf("account credits = %v, want 2", updated.Credits)
			}
		})
	}
}
