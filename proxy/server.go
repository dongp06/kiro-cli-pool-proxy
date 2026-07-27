package proxy

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
	"kiro-cli-pool-proxy/pool"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Server is a plain HTTP reverse-proxy for Kiro CLI.
//
// Kiro CLI is pointed at this proxy via its built-in endpoint settings
// (no MITM, no cert, no fork required):
//
//	kiro-cli settings api.krs.service '{"endpoint":"http://PROXY","region":"us-east-1"}'
//	kiro-cli settings api.cps.service '{"endpoint":"http://PROXY","region":"us-east-1"}'
//
// The proxy receives plain HTTP requests (the CLI already built the perfect
// Kiro payload), swaps the auth credentials for a pool account, forwards to the
// real Kiro endpoint, and tees the response to count credits.
type Server struct {
	pool   *pool.Pool
	cfg    *config.Config
	client *http.Client
	admin  *AdminHandler
	logs   *LogStore
	hub    *EventHub
}

// NewServer creates a plain reverse-proxy server.
func NewServer(cfg *config.Config, p *pool.Pool) *Server {
	logs := NewLogStore(500)
	hub := NewEventHub()
	logs.SetHub(hub)
	return &Server{
		pool: p,
		cfg:  cfg,
		logs: logs,
		hub:  hub,
		admin: NewAdminHandler(cfg, p, logs, hub),
		client: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				MaxIdleConns:          50,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				DisableCompression:    true, // keep binary event stream intact
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ServeHTTP handles every proxied Kiro request.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Admin web panel + API.
	if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
		s.admin.ServeHTTP(w, r)
		return
	}

	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		w.WriteHeader(200)
		io.WriteString(w, "ok")
		return
	}

	// Anthropic Messages API (Claude Code CLI, opencode). Accept both the bare
	// path and any /v1/messages variant (some clients append query/beta flags).
	if strings.HasPrefix(r.URL.Path, "/v1/messages/count_tokens") || strings.HasPrefix(r.URL.Path, "/anthropic/v1/messages/count_tokens") {
		s.serveAnthropicCountTokens(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/messages") || strings.HasPrefix(r.URL.Path, "/anthropic/v1/messages") {
		s.serveAnthropic(w, r)
		return
	}

	// OpenAI Chat Completions API (Codex CLI, opencode).
	if strings.HasPrefix(r.URL.Path, "/v1/chat/completions") || strings.HasPrefix(r.URL.Path, "/openai/v1/chat/completions") {
		s.serveOpenAI(w, r)
		return
	}
	// OpenAI models stub (some clients probe this on startup).
	if r.Method == http.MethodGet && (r.URL.Path == "/v1/models" || r.URL.Path == "/openai/v1/models") {
		s.serveOpenAIModels(w, r)
		return
	}

	// Zero-login client bootstrap: `curl -fsSL http://PROXY/setup-client.sh | bash -s -- http://PROXY REGION`
	if r.Method == http.MethodGet && r.URL.Path == "/setup-client.sh" {
		if data, ok := readSetupClientScript(); ok {
			w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
			w.Write(data)
		} else {
			http.Error(w, "setup-client.sh not found on server", http.StatusNotFound)
		}
		return
	}

	// Optional shared-secret guard (for remote deployments). The CLI can send it
	// via a custom header configured through settings, or leave empty for local.
	if s.cfg.PoolKey != "" && r.Header.Get("X-Pool-Key") != s.cfg.PoolKey {
		// Do not hard-fail chat when key is absent on localhost; only enforce
		// when a key is configured AND provided-but-wrong or missing.
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// API key is mandatory: kiro-cli sends the seeded key as
	// `Authorization: Bearer <key>`. Validate before swapping the pool token.
	var apiKeyID, keyLabel string
	{
		bearer := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		id, ok := s.cfg.ValidateAPIKey(bearer)
		if !ok {
			http.Error(w, "invalid or missing API key", http.StatusUnauthorized)
			return
		}
		if s.cfg.APIKeyOverCredit(id) {
			http.Error(w, "API key credit limit reached", http.StatusPaymentRequired)
			return
		}
		apiKeyID = id
		keyLabel = id
		if k, ok := s.cfg.GetAPIKey(id); ok && strings.TrimSpace(k.Name) != "" {
			keyLabel = k.Name
		}
	}

	// Read the request body (the CLI's fully-formed Kiro payload).
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	amzTarget := r.Header.Get("X-Amz-Target")
	isChat := strings.Contains(amzTarget, "GenerateAssistantResponse") ||
		strings.Contains(amzTarget, "StreamingService")

	// Ground-truth capture (RE): dump full request (headers + body) for chat
	// turns so we can build an accurate Anthropic/OpenAI translation layer.
	if dir := os.Getenv("KPP_CAPTURE"); dir != "" && isChat {
		var sb strings.Builder
		sb.WriteString(r.Method + " " + r.URL.Path + "\n")
		for k, v := range r.Header {
			if strings.EqualFold(k, "Authorization") {
				sb.WriteString(k + ": <redacted>\n")
				continue
			}
			sb.WriteString(k + ": " + strings.Join(v, ",") + "\n")
		}
		sb.WriteString("\n")
		sb.Write(body)
		_ = os.WriteFile(filepath.Join(dir, "chat-request.txt"),
			[]byte(sb.String()), 0600)
	}
	isUsage := strings.Contains(amzTarget, "GetUsageLimits")

	if os.Getenv("KPP_DEBUG_REQ") != "" {
		log.Printf("[req] method=%s path=%s target=%q isChat=%v isUsage=%v bodyLen=%d hasProfileArn=%v",
			r.Method, r.URL.Path, amzTarget, isChat, isUsage, len(body),
			strings.Contains(string(body), "profileArn"))
	}

	excluded := make(map[string]bool)
	retryLimit := s.pool.RetryLimit()

	for attempt := 0; attempt < retryLimit; attempt++ {
		account := s.pool.GetNext(excluded)
		if account == nil {
			if isChat {
				s.logs.Add(LogEntry{TimeUnix: time.Now().Unix(), Account: "-", ApiKey: keyLabel, Status: 503, Kind: "chat", Err: "no available accounts in pool"})
			}
			http.Error(w, "no available accounts in pool", http.StatusServiceUnavailable)
			return
		}

		// Ensure the account token is fresh.
		if auth.NeedsRefresh(account) {
			if err := auth.RefreshToken(account); err != nil {
				log.Printf("[proxy] refresh %s failed: %v", account.ID, err)
				s.pool.RecordFailure(account.ID, 401, "refresh failed")
				excluded[account.ID] = true
				continue
			}
			s.cfg.UpdateAccountToken(account.ID, account.AccessToken, account.RefreshToken, account.ExpiresAt)
		}

		// Rewrite profileArn in the body for this account.
		newBody := RewriteProfileArn(body, account.ProfileArn)
		if isChat && account.AuthMethod == "api_key" {
			// ksk keys have no profileArn; the CodeWhisperer data-plane rejects
			// a stray one, so strip whatever the client sent.
			newBody = RemoveProfileArn(newBody)
		}

		// RE/debug: dump the (pre-swap) chat request body once.
		if dbg := os.Getenv("KPP_DEBUG_BODY"); dbg != "" && isChat {
			_ = os.WriteFile(dbg, body, 0600)
		}

		// Resolve region + upstream host.
		region := RegionFromProfileArn(account.ProfileArn)
		if region == "" {
			region = account.Region
		}
		if region == "" {
			region = "us-east-1"
		}
		var upstreamHost string
		var upstreamURL string
		if isChat && account.AuthMethod == "api_key" {
			// API keys are region-bound and only accepted at the CodeWhisperer
			// data-plane (q.{region}.amazonaws.com/generateAssistantResponse),
			// not runtime.*.kiro.dev. Rewrite host + path accordingly.
			u := apiKeyChatURL(region)
			upstreamHost = hostFromURL(u)
			upstreamURL = u
		} else {
			if isChat {
				upstreamHost = runtimeHostForRegion(region)
			} else {
				upstreamHost = managementHostForRegion(region)
			}
			upstreamURL = "https://" + upstreamHost + r.URL.Path
			if r.URL.RawQuery != "" {
				upstreamURL += "?" + r.URL.RawQuery
			}
		}

		upReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL,
			strings.NewReader(string(newBody)))
		if err != nil {
			http.Error(w, "build upstream request", http.StatusInternalServerError)
			return
		}

		// Copy client headers verbatim (User-Agent, x-amz-*, optout, etc.).
		for k, v := range r.Header {
			if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") {
				continue
			}
			upReq.Header[k] = v
		}
		// Swap credentials for the pool account.
		upReq.Header.Set("Authorization", "Bearer "+account.AccessToken)
		upReq.Header.Set("Host", upstreamHost)
		upReq.Host = upstreamHost
		// API-key chat hits the CodeWhisperer data-plane, which expects
		// application/json rather than the CLI's x-amz-json-1.0 content type.
		if isChat && account.AuthMethod == "api_key" {
			upReq.Header.Set("Content-Type", "application/json")
		}
		if tt := TokenTypeHeader(account.AuthMethod); tt != "" {
			upReq.Header.Set("tokentype", tt)
		} else {
			upReq.Header.Del("tokentype")
		}

		resp, err := s.client.Do(upReq)
		if err != nil {
			log.Printf("[proxy] upstream error (account %s): %v", account.ID, err)
			s.pool.RecordFailure(account.ID, 0, err.Error())
			excluded[account.ID] = true
			continue
		}

		// Retry on account-level failures.
		if resp.StatusCode == 429 || resp.StatusCode == 401 ||
			resp.StatusCode == 403 || resp.StatusCode >= 500 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[proxy] upstream %d (account %s): %s",
				resp.StatusCode, account.ID, truncate(string(b), 200))
			s.pool.RecordFailure(account.ID, resp.StatusCode, string(b))
			excluded[account.ID] = true
			continue
		}

		// Success. Stream response back verbatim while tee-parsing credits.
		s.pool.RecordSuccess(account.ID)
		s.streamResponse(w, resp, account, region, isChat, isUsage, apiKeyID, keyLabel)
		return
	}

	if isChat {
		s.logs.Add(LogEntry{TimeUnix: time.Now().Unix(), Account: "-", ApiKey: keyLabel, Status: 503, Kind: "chat", Err: "all accounts exhausted after retries"})
	}
	http.Error(w, "all accounts exhausted after retries", http.StatusServiceUnavailable)
}

// streamResponse copies upstream → client byte-for-byte, tee-parsing credit
// usage from meteringEvent frames (only for chat/streaming responses).
func (s *Server) streamResponse(w http.ResponseWriter, resp *http.Response, account *config.Account, region string, isChat, isUsage bool, apiKeyID, keyLabel string) {
	defer resp.Body.Close()

	// /usage → rewrite GetUsageLimits so the client sees THEIR api-key credit
	// budget, not the shared pool account's real quota.
	if isUsage && apiKeyID != "" && resp.StatusCode == 200 {
		if k, ok := s.cfg.GetAPIKey(apiKeyID); ok && k.CreditLimit > 0 {
			ok := s.rewriteUsageResponse(w, resp, k)
			if os.Getenv("KPP_DEBUG_REQ") != "" {
				log.Printf("[usage] rewrite handled=%v status=%d contentEncoding=%q",
					ok, resp.StatusCode, resp.Header.Get("Content-Encoding"))
			}
			if ok {
				return
			}
		}
	} else if isUsage && os.Getenv("KPP_DEBUG_REQ") != "" {
		log.Printf("[usage] SKIP rewrite: apiKeyID=%q status=%d", apiKeyID, resp.StatusCode)
	}

	// Propagate upstream headers + status.
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)

	var reader io.Reader = resp.Body
	sink := &MeteringSink{}
	if isChat {
		reader = io.TeeReader(resp.Body, sink)
	}

	// Ground-truth capture (RE): tee the raw chat response bytes to a file so we
	// can decode assistantResponseEvent/toolUseEvent payload shapes offline.
	var capBuf *bytes.Buffer
	if dir := os.Getenv("KPP_CAPTURE"); dir != "" && isChat {
		capBuf = &bytes.Buffer{}
		reader = io.TeeReader(reader, capBuf)
	}

	// Stream in chunks so SSE/event-stream flushes promptly to the CLI.
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}

	if capBuf != nil {
		_ = os.WriteFile(filepath.Join(os.Getenv("KPP_CAPTURE"), "chat-response.bin"),
			capBuf.Bytes(), 0600)
	}

	// Record usage after the turn.
	if isChat {
		// Credits (from meteringEvent) are the cost metric; the quota is measured
		// in INVOCATIONS (request count) per GetUsageLimits AGENTIC_REQUEST. So
		// track credits for accounting, but increment the quota counter by 1
		// invocation. The 5-min GetUsageLimits poll re-syncs the true value.
		s.cfg.RecordUsage(account.ID, sink.Credits) // Credits + Requests++
		s.cfg.UpdateQuotaCurrentDelta(account.ID, 1) // one invocation
		if apiKeyID != "" {
			s.cfg.RecordKeyUsage(apiKeyID, sink.Credits)
		}
		if s.hub != nil {
			if updated, ok := s.cfg.GetAccountByID(account.ID); ok {
				s.hub.Publish(Event{Type: "quota", Data: toView(updated)})
			}
		}

		acctLabel := account.Email
		if acctLabel == "" {
			acctLabel = account.ID
		}
		s.logs.Add(LogEntry{
			TimeUnix: time.Now().Unix(), Account: acctLabel, ApiKey: keyLabel,
			Status: resp.StatusCode, Credits: sink.Credits, Kind: "chat",
		})
		ctxInfo := ""
		if sink.SawContext {
			ctxInfo = fmt.Sprintf(" ctx=%.0f%%", sink.ContextPct)
		}
		meterInfo := ""
		if !sink.SawMetering {
			meterInfo = " (no meteringEvent)"
		}
		log.Printf("[proxy] OK account=%s region=%s credits=%.4f%s%s",
			acctLabel, region, sink.Credits, ctxInfo, meterInfo)
		if os.Getenv("KPP_DEBUG_BODY") != "" && len(sink.Types) > 0 {
			log.Printf("[re] response event-types: %v", sink.Types)
		}
	}
}

// rewriteUsageResponse rewrites a GetUsageLimits JSON body so the client's
// /usage shows the API key's own credit budget (credits used / creditLimit)
// instead of the shared pool account's real quota. Returns true if it handled
// the response (wrote to w). Falls back (returns false) on any parse issue.
func (s *Server) rewriteUsageResponse(w http.ResponseWriter, resp *http.Response, k config.APIKey) bool {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}
	body := raw
	gz := strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip")
	if gz {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return false
		}
		if body, err = io.ReadAll(zr); err != nil {
			return false
		}
	}

	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return false
	}
	arr, ok := m["usageBreakdownList"].([]any)
	if !ok {
		return false
	}
	keyReset := keyResetUnix(k)
	for _, e := range arr {
		if o, ok := e.(map[string]any); ok {
			// The kiro-cli /usage panel renders the *WithPrecision fields for
			// KIRO POWER plans (currentUsageWithPrecision / usageLimitWithPrecision).
			// IMPORTANT: the plain currentUsage/usageLimit fields are INTEGERS
			// (invocation counts) in the upstream schema — the CLI deserializes
			// them as integers, so a float here breaks parsing and the panel
			// hangs on "Loading usage data...". Keep plain fields integer and
			// put the fractional credit values only in the *WithPrecision fields.
			o["currentUsage"] = int64(k.Credits)
			o["usageLimit"] = int64(k.CreditLimit)
			o["currentUsageWithPrecision"] = k.Credits
			o["usageLimitWithPrecision"] = k.CreditLimit
			// Zero out any overage so the panel never leaks pool-account overages.
			o["currentOveragesWithPrecision"] = 0
			o["overageCap"] = int64(0)
			o["overageCapWithPrecision"] = 0
			o["overageCredits"] = []any{}
			o["nextDateReset"] = keyReset
			delete(o, "currentOverages")
			delete(o, "overageCharges")
		}
	}

	// Scrub pool-account identity so an API-key client never sees which real
	// account backs it. userInfo.userId is the account's true directory ID.
	// Keep the object shape intact (deleting it can make the CLI hang on
	// deserialize) but replace the id with a synthetic per-key value.
	if ui, ok := m["userInfo"].(map[string]any); ok {
		ui["userId"] = "kpp-" + k.ID
	}

	// Rebrand the subscription block so the /usage panel reads as an API-key
	// budget rather than the pool account's real Kiro plan. Only the title
	// string is changed here — enum fields (`type`, overageStatus,
	// subscriptionManagementTarget) are left intact to avoid deserialize
	// failures that would hang the panel. The org-footer / reset-date tweaks
	// are handled as a separate, verified step.
	if si, ok := m["subscriptionInfo"].(map[string]any); ok {
		title := "API KEY"
		if strings.TrimSpace(k.Name) != "" {
			title = "API KEY: " + k.Name
		}
		si["subscriptionTitle"] = title
		// NOTE: the "contact your account administrator" footer is driven by the
		// client's own auth/login type (IdC/SSO), not by this response, so it
		// cannot be suppressed here. Leaving subscriptionManagementTarget as-is.
	}

	// Top-level reset date drives the "resets on <date>" header. Align it with
	// the API key's own 30-day billing cycle instead of the pool account's.
	if _, has := m["nextDateReset"]; has {
		m["nextDateReset"] = keyReset
	}

	out, err := json.Marshal(m)
	if err != nil {
		return false
	}

	// Copy headers, but we send uncompressed JSON with a fresh length.
	for hk, hv := range resp.Header {
		w.Header()[hk] = hv
	}
	w.Header().Del("Content-Encoding")
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(resp.StatusCode)
	w.Write(out)
	return true
}
// keyResetUnix computes the next credit-reset timestamp for an API key.
// Credits reset on a rolling 30-day cycle anchored to the key's creation date;
// the returned value is the next cycle boundary at or after now (unix seconds).
func keyResetUnix(k config.APIKey) int64 {
	const period = int64(30 * 24 * 3600)
	created := k.CreatedUnix
	if created <= 0 {
		created = time.Now().Unix()
	}
	now := time.Now().Unix()
	reset := created
	if reset <= now {
		cycles := (now-created)/period + 1
		reset = created + cycles*period
	}
	return reset
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// readSetupClientScript locates setup-client.sh next to the executable or in the
// current working directory and returns its contents.
func readSetupClientScript() ([]byte, bool) {
	candidates := []string{"setup-client.sh"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "setup-client.sh"))
	}
	for _, p := range candidates {
		if data, err := os.ReadFile(p); err == nil {
			return data, true
		}
	}
	return nil, false
}
