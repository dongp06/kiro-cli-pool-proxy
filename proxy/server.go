package proxy

import (
	"fmt"
	"io"
	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
	"kiro-cli-pool-proxy/pool"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
}

// NewServer creates a plain reverse-proxy server.
func NewServer(cfg *config.Config, p *pool.Pool) *Server {
	return &Server{
		pool:  p,
		cfg:   cfg,
		admin: NewAdminHandler(cfg, p),
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

	// API-key auth: kiro-cli sends the seeded token as `Authorization: Bearer <key>`.
	// Validate it here (before we swap in the pool account's real token).
	var apiKeyID string
	if s.cfg.GetRequireAPIKey() {
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

	excluded := make(map[string]bool)
	retryLimit := s.pool.RetryLimit()

	for attempt := 0; attempt < retryLimit; attempt++ {
		account := s.pool.GetNext(excluded)
		if account == nil {
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

		// Resolve region + upstream host.
		region := RegionFromProfileArn(account.ProfileArn)
		if region == "" {
			region = account.Region
		}
		if region == "" {
			region = "us-east-1"
		}
		var upstreamHost string
		if isChat {
			upstreamHost = runtimeHostForRegion(region)
		} else {
			upstreamHost = managementHostForRegion(region)
		}
		upstreamURL := "https://" + upstreamHost + r.URL.Path
		if r.URL.RawQuery != "" {
			upstreamURL += "?" + r.URL.RawQuery
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
		s.streamResponse(w, resp, account, region, isChat, apiKeyID)
		return
	}

	http.Error(w, "all accounts exhausted after retries", http.StatusServiceUnavailable)
}

// streamResponse copies upstream → client byte-for-byte, tee-parsing credit
// usage from meteringEvent frames (only for chat/streaming responses).
func (s *Server) streamResponse(w http.ResponseWriter, resp *http.Response, account *config.Account, region string, isChat bool, apiKeyID string) {
	defer resp.Body.Close()

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

		acctLabel := account.Email
		if acctLabel == "" {
			acctLabel = account.ID
		}
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
	}
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
