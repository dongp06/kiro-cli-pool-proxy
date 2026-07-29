package proxy

import (
	"kiro-cli-pool-proxy/config"
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

	s.recordChatUsage(&acc, "key1", 1.5, 42)

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

	s.recordChatUsage(&acc, "key1", 0, 0)

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
