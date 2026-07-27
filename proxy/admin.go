package proxy

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
	"kiro-cli-pool-proxy/kirolocal"
	"kiro-cli-pool-proxy/pool"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AdminHandler serves the web admin panel (API + embedded static assets).
type AdminHandler struct {
	cfg  *config.Config
	pool *pool.Pool
	logs *LogStore
	hub  *EventHub
	dist fs.FS // built frontend (proxy/webdist)

	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiry

	// Login brute-force throttle, keyed by client IP.
	loginFails map[string]*loginAttempts
}

// loginAttempts tracks failed admin logins from one client IP so we can lock it
// out after too many misses within a window.
type loginAttempts struct {
	count       int
	last        time.Time
	lockedUntil time.Time
}

const (
	maxLoginFails = 5                // failures before lockout
	loginLockout  = 5 * time.Minute  // lockout duration once tripped
	loginWindow   = 15 * time.Minute // idle window after which the counter resets
)

// NewAdminHandler creates the admin panel handler.
func NewAdminHandler(cfg *config.Config, p *pool.Pool, logs *LogStore, hub *EventHub) *AdminHandler {
	sub, _ := fs.Sub(adminDist, "webdist")
	a := &AdminHandler{cfg: cfg, pool: p, logs: logs, hub: hub, dist: sub, sessions: make(map[string]time.Time), loginFails: make(map[string]*loginAttempts)}
	go a.gcSessions()
	return a
}

const adminCookie = "kpp_admin"

// adminSecurityHeaders applies defensive headers to every admin response.
// The panel is a self-contained SPA (no external scripts, styles, or frames),
// so a strict same-origin CSP is safe and blocks injected/remote content.
func adminSecurityHeaders(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy",
		"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if isTLS(r) {
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

func (a *AdminHandler) gcSessions() {
	for range time.Tick(10 * time.Minute) {
		a.mu.Lock()
		now := time.Now()
		for tok, exp := range a.sessions {
			if now.After(exp) {
				delete(a.sessions, tok)
			}
		}
		a.mu.Unlock()
	}
}

func (a *AdminHandler) newSession() string {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	a.mu.Lock()
	a.sessions[tok] = time.Now().Add(12 * time.Hour)
	a.mu.Unlock()
	return tok
}

func (a *AdminHandler) validSession(tok string) bool {
	if tok == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[tok]
	return ok && time.Now().Before(exp)
}

// clientIP extracts a best-effort client identifier for rate-limiting. It does
// NOT trust X-Forwarded-For by default (spoofable) — the remote address is the
// only value an attacker cannot forge on a direct connection.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginLocked reports whether the given IP is currently locked out, and if so
// how long remains. Expired counters are pruned lazily.
func (a *AdminHandler) loginLocked(ip string) (bool, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	att := a.loginFails[ip]
	if att == nil {
		return false, 0
	}
	now := time.Now()
	if !att.lockedUntil.IsZero() && now.Before(att.lockedUntil) {
		return true, att.lockedUntil.Sub(now)
	}
	return false, 0
}

// registerLoginFail records a failed attempt for the IP and trips a lockout once
// the threshold is crossed.
func (a *AdminHandler) registerLoginFail(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	att := a.loginFails[ip]
	if att == nil || now.Sub(att.last) > loginWindow {
		att = &loginAttempts{}
		a.loginFails[ip] = att
	}
	att.last = now
	att.count++
	if att.count >= maxLoginFails {
		att.lockedUntil = now.Add(loginLockout)
		att.count = 0
	}
}

// clearLoginFails resets the throttle for an IP after a successful login.
func (a *AdminHandler) clearLoginFails(ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.loginFails, ip)
}

// authed returns true if the request carries a valid session, OR if admin auth
// is disabled (no password configured).
func (a *AdminHandler) authed(r *http.Request) bool {
	if a.cfg.GetAdminPassword() == "" {
		return true // auth disabled
	}
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return false
	}
	return a.validSession(c.Value)
}

// ServeHTTP routes /admin and /admin/api/*.
func (a *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	adminSecurityHeaders(w, r)
	path := r.URL.Path

	// API routes
	if strings.HasPrefix(path, "/admin/api/") {
		a.serveAPI(w, r, strings.TrimPrefix(path, "/admin/api/"))
		return
	}

	// Static assets from the built frontend (no SPA fallback).
	rel := strings.TrimPrefix(strings.TrimPrefix(path, "/admin"), "/")
	if rel == "" {
		rel = "index.html"
	}
	data, err := fs.ReadFile(a.dist, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(rel)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Write(data)
}

func (a *AdminHandler) serveAPI(w http.ResponseWriter, r *http.Request, route string) {
	w.Header().Set("Content-Type", "application/json")

	// Login is unauthenticated.
	if route == "login" && r.Method == http.MethodPost {
		a.apiLogin(w, r)
		return
	}
	if route == "auth" && r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{
			"authRequired": a.cfg.GetAdminPassword() != "",
			"authed":       a.authed(r),
		})
		return
	}

	// Everything else requires auth.
	if !a.authed(r) {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}

	switch {
	case route == "logout" && r.Method == http.MethodPost:
		if c, err := r.Cookie(adminCookie); err == nil {
			a.mu.Lock()
			delete(a.sessions, c.Value)
			a.mu.Unlock()
		}
		writeJSON(w, 200, map[string]bool{"ok": true})

	case route == "overview" && r.Method == http.MethodGet:
		a.apiOverview(w, r)

	case route == "accounts" && r.Method == http.MethodGet:
		a.apiListAccounts(w, r)

	case route == "accounts" && r.Method == http.MethodPost:
		a.apiAddAccount(w, r)

	case route == "accounts/import-local" && r.Method == http.MethodPost:
		a.apiImportLocal(w, r)

	case strings.HasPrefix(route, "accounts/") && strings.HasSuffix(route, "/test") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(route, "accounts/"), "/test")
		a.apiTestAccount(w, r, id)

	case strings.HasPrefix(route, "accounts/") && strings.HasSuffix(route, "/refresh-quota") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(route, "accounts/"), "/refresh-quota")
		a.apiRefreshQuota(w, r, id)

	case strings.HasPrefix(route, "accounts/") && r.Method == http.MethodPatch:
		a.apiToggleAccount(w, r, strings.TrimPrefix(route, "accounts/"))

	case strings.HasPrefix(route, "accounts/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(route, "accounts/")
		if a.cfg.RemoveAccount(id) {
			a.cfg.Save()
			a.pool.Reload()
			a.publishAccounts()
			writeJSON(w, 200, map[string]bool{"ok": true})
		} else {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}

	case route == "settings" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{
			"strategy":      a.cfg.GetStrategy(),
			"listenAddr":    a.cfg.GetListenAddr(),
			"adminPassword": a.cfg.GetAdminPassword() != "",
			"passwordEnv":   config.AdminPasswordFromEnv(),
		})

	case route == "settings/password" && r.Method == http.MethodPost:
		a.apiSetPassword(w, r)

	case route == "settings" && r.Method == http.MethodPatch:
		var body struct {
			Strategy string `json:"strategy"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Strategy == "round-robin" || body.Strategy == "smart" {
			a.cfg.SetStrategy(body.Strategy)
		}
		a.cfg.Save()
		writeJSON(w, 200, map[string]any{"strategy": a.cfg.GetStrategy()})

	case route == "keys" && r.Method == http.MethodGet:
		writeJSON(w, 200, a.cfg.APIKeysSnapshot())

	case route == "keys" && r.Method == http.MethodPost:
		var body struct {
			Name        string  `json:"name"`
			CreditLimit float64 `json:"creditLimit"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		idb := make([]byte, 4)
		kb := make([]byte, 24)
		rand.Read(idb)
		rand.Read(kb)
		k := config.APIKey{
			ID:          "key-" + hex.EncodeToString(idb),
			Name:        strings.TrimSpace(body.Name),
			Key:         "kpp_" + hex.EncodeToString(kb),
			Enabled:     true,
			CreditLimit: body.CreditLimit,
			CreatedUnix: time.Now().Unix(),
		}
		a.cfg.AddAPIKey(k)
		a.cfg.Save()
		writeJSON(w, 200, k)

	case strings.HasPrefix(route, "keys/") && r.Method == http.MethodPatch:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if a.cfg.SetAPIKeyEnabled(strings.TrimPrefix(route, "keys/"), body.Enabled) {
			a.cfg.Save()
			writeJSON(w, 200, map[string]bool{"ok": true})
		} else {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}

	case strings.HasPrefix(route, "keys/") && r.Method == http.MethodDelete:
		if a.cfg.RemoveAPIKey(strings.TrimPrefix(route, "keys/")) {
			a.cfg.Save()
			writeJSON(w, 200, map[string]bool{"ok": true})
		} else {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}

	case route == "logs" && r.Method == http.MethodGet:
		writeJSON(w, 200, a.logs.Snapshot())

	case route == "logs" && r.Method == http.MethodDelete:
		a.logs.Clear()
		writeJSON(w, 200, map[string]bool{"ok": true})

	case route == "events" && r.Method == http.MethodGet:
		a.apiEvents(w, r)

	case route == "models/sync" && r.Method == http.MethodPost:
		a.apiSyncModels(w, r)

	case route == "models" && r.Method == http.MethodGet:
		writeJSON(w, 200, EffectiveKiroModels())

	case strings.HasPrefix(route, "accounts/") && strings.HasSuffix(route, "/test-model") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(route, "accounts/"), "/test-model")
		a.apiTestModel(w, r, id)

	// --- Kiro login flows ---
	case route == "auth/iam-sso/start" && r.Method == http.MethodPost:
		a.apiStartIamSso(w, r)

	case route == "auth/iam-sso/complete" && r.Method == http.MethodPost:
		a.apiCompleteIamSso(w, r)

	case route == "auth/builderid/start" && r.Method == http.MethodPost:
		a.apiStartBuilderId(w, r)

	case route == "auth/builderid/poll" && r.Method == http.MethodPost:
		a.apiPollBuilderId(w, r)

	case route == "auth/sso-token" && r.Method == http.MethodPost:
		a.apiImportSsoToken(w, r)

	case route == "auth/microsoft-sso/start" && r.Method == http.MethodPost:
		a.apiStartMicrosoftSso(w, r)

	case route == "auth/microsoft-sso/complete" && r.Method == http.MethodPost:
		a.apiCompleteMicrosoftSso(w, r)

	case route == "auth/microsoft-sso/cancel" && r.Method == http.MethodPost:
		a.apiCancelMicrosoftSso(w, r)

	case route == "auth/api-key" && r.Method == http.MethodPost:
		a.apiImportApiKey(w, r)

	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

func (a *AdminHandler) apiLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if locked, remain := a.loginLocked(ip); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(remain.Seconds())+1))
		writeJSON(w, 429, map[string]string{"error": "too many attempts, try again later"})
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	pw := a.cfg.GetAdminPassword()
	if pw == "" {
		// No password set — issue a session anyway.
		a.setSessionCookie(w, r, a.newSession())
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(pw)) != 1 {
		a.registerLoginFail(ip)
		writeJSON(w, 401, map[string]string{"error": "wrong password"})
		return
	}
	a.clearLoginFails(ip)
	a.setSessionCookie(w, r, a.newSession())
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// apiSetPassword sets or changes the admin panel password. Requires the current
// password when one is already set (an authenticated session alone is not
// enough — this blocks a hijacked session from silently locking out the owner).
// Refused when the password is pinned by the environment.
func (a *AdminHandler) apiSetPassword(w http.ResponseWriter, r *http.Request) {
	if config.AdminPasswordFromEnv() {
		writeJSON(w, 409, map[string]string{"error": "password is set via environment and cannot be changed here"})
		return
	}
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if cur := a.cfg.GetAdminPassword(); cur != "" {
		if subtle.ConstantTimeCompare([]byte(body.Current), []byte(cur)) != 1 {
			writeJSON(w, 403, map[string]string{"error": "current password is wrong"})
			return
		}
	}
	newPw := strings.TrimSpace(body.New)
	if newPw != "" && len(newPw) < 8 {
		writeJSON(w, 400, map[string]string{"error": "password must be at least 8 characters"})
		return
	}
	a.cfg.SetAdminPassword(newPw)
	a.cfg.Save()

	// Invalidate all sessions so a stolen cookie can't outlive a rotation, then
	// re-issue a fresh session for this caller (if a password remains set).
	a.mu.Lock()
	a.sessions = make(map[string]time.Time)
	a.mu.Unlock()
	if newPw != "" {
		a.setSessionCookie(w, r, a.newSession())
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// isTLS reports whether the request reached us over HTTPS, either directly or
// via a terminating proxy that set X-Forwarded-Proto.
func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *AdminHandler) setSessionCookie(w http.ResponseWriter, r *http.Request, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    tok,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   12 * 3600,
	})
}

// accountView is the sanitized account shape sent to the UI (no secrets).
type accountView struct {
	ID            string  `json:"id"`
	Email         string  `json:"email"`
	AuthMethod    string  `json:"authMethod"`
	Region        string  `json:"region"`
	Enabled       bool    `json:"enabled"`
	Credits       float64 `json:"credits"`
	Requests      int64   `json:"requests"`
	LastUsedUnix  int64   `json:"lastUsedUnix"`
	UsageLimit    float64 `json:"usageLimit"`
	UsageCurrent  float64 `json:"usageCurrent"`
	NextResetUnix int64   `json:"nextResetUnix"`
	HasProfileArn bool    `json:"hasProfileArn"`
	TokenExpires  int64   `json:"tokenExpires"`
	Plan          string  `json:"plan"`
}

func toView(a config.Account) accountView {
	return accountView{
		ID: a.ID, Email: a.Email, AuthMethod: a.AuthMethod, Region: a.Region,
		Enabled: a.Enabled, Credits: a.Credits, Requests: a.Requests,
		LastUsedUnix: a.LastUsedUnix, UsageLimit: a.UsageLimit, UsageCurrent: a.UsageCurrent,
		NextResetUnix: a.NextResetUnix, HasProfileArn: strings.TrimSpace(a.ProfileArn) != "",
		TokenExpires: a.ExpiresAt, Plan: a.Plan,
	}
}

func (a *AdminHandler) apiListAccounts(w http.ResponseWriter, r *http.Request) {
	snap := a.cfg.UsageSnapshot()
	views := make([]accountView, 0, len(snap))
	for _, acc := range snap {
		views = append(views, toView(acc))
	}
	writeJSON(w, 200, views)
}

func (a *AdminHandler) apiOverview(w http.ResponseWriter, r *http.Request) {
	snap := a.cfg.UsageSnapshot()
	var enabled int
	var totalCredits float64
	var totalReq int64
	var quotaUsed, quotaLimit float64
	for _, acc := range snap {
		if acc.Enabled {
			enabled++
		}
		totalCredits += acc.Credits
		totalReq += acc.Requests
		quotaUsed += acc.UsageCurrent
		quotaLimit += acc.UsageLimit
	}
	writeJSON(w, 200, map[string]any{
		"totalAccounts": len(snap),
		"enabled":       enabled,
		"available":     a.pool.AvailableCount(),
		"totalCredits":  totalCredits,
		"totalRequests": totalReq,
		"quotaUsed":     quotaUsed,
		"quotaLimit":    quotaLimit,
		"strategy":      a.cfg.GetStrategy(),
	})
}

func (a *AdminHandler) apiAddAccount(w http.ResponseWriter, r *http.Request) {
	var acc config.Account
	if err := json.NewDecoder(r.Body).Decode(&acc); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(acc.ID) == "" {
		b := make([]byte, 6)
		rand.Read(b)
		acc.ID = "acc-" + hex.EncodeToString(b)
	}
	if acc.AuthMethod == "" {
		acc.AuthMethod = "idc"
	}
	acc.Enabled = true
	a.cfg.AddAccount(acc)
	a.cfg.Save()
	a.pool.Reload()
	a.publishAccounts()
	writeJSON(w, 200, toView(acc))
}

func (a *AdminHandler) apiToggleAccount(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if a.cfg.SetAccountEnabled(id, body.Enabled) {
		a.cfg.Save()
		a.pool.Reload()
		a.publishAccounts()
		writeJSON(w, 200, map[string]bool{"ok": true})
	} else {
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

func (a *AdminHandler) apiImportLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path       string `json:"path"`
		ProfileArn string `json:"profileArn"`
		Region     string `json:"region"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	acc, err := kirolocal.ImportAccount(body.Path)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if body.ProfileArn != "" {
		acc.ProfileArn = body.ProfileArn
	}
	if body.Region != "" {
		acc.Region = body.Region
	}
	a.cfg.AddAccount(*acc)
	a.cfg.Save()
	a.pool.Reload()
	a.publishAccounts()
	writeJSON(w, 200, toView(*acc))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// prepareAccount refreshes an expiring token and resolves a missing profile ARN,
// persisting both back to config. Returns the up-to-date account copy.
func (a *AdminHandler) prepareAccount(id string) (config.Account, error) {
	acc, ok := a.cfg.GetAccountByID(id)
	if !ok {
		return config.Account{}, fmt.Errorf("not found")
	}
	if auth.NeedsRefresh(&acc) {
		if err := auth.RefreshToken(&acc); err != nil {
			return acc, fmt.Errorf("token refresh failed: %w", err)
		}
		a.cfg.UpdateAccountToken(acc.ID, acc.AccessToken, acc.RefreshToken, acc.ExpiresAt)
	}
	// api_key (ksk_) accounts are region-bound and have no profileArn; the
	// control-plane GET form authenticates by key alone. Skip ARN resolution.
	if acc.AuthMethod != "api_key" && strings.TrimSpace(acc.ProfileArn) == "" {
		arn, err := auth.ResolveProfileArn(&acc)
		if err != nil {
			return acc, fmt.Errorf("resolve profileArn: %w", err)
		}
		acc.ProfileArn = arn
		if acc.Region == "" {
			acc.Region = RegionFromProfileArn(arn)
		}
		a.cfg.SetAccountProfileArn(acc.ID, arn)
	}
	a.cfg.Save()
	return acc, nil
}

// apiTestAccount validates an account by resolving its ARN and calling
// GetUsageLimits — the same path the proxy relies on. Returns ok/error.
func (a *AdminHandler) apiTestAccount(w http.ResponseWriter, r *http.Request, id string) {
	acc, err := a.prepareAccount(id)
	if err != nil {
		if err.Error() == "not found" {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if _, err := auth.FetchUsageLimits(&acc); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	a.pool.Reload()
	writeJSON(w, 200, map[string]any{"ok": true, "email": acc.Email, "hasProfileArn": true})
}

// apiRefreshQuota resolves the ARN if needed then refreshes the account's quota
// snapshot from GetUsageLimits. Returns the updated account view.
func (a *AdminHandler) apiRefreshQuota(w http.ResponseWriter, r *http.Request, id string) {
	acc, err := a.prepareAccount(id)
	if err != nil {
		if err.Error() == "not found" {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	resp, err := auth.FetchUsageLimits(&acc)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	limit, current, nextReset := auth.QuotaFromUsage(resp)
	plan := auth.PlanFromUsage(resp)
	if limit > 0 || plan != "" {
		a.cfg.UpdateQuota(acc.ID, limit, current, nextReset, plan)
		a.cfg.Save()
	}
	if updated, ok := a.cfg.GetAccountByID(id); ok {
		acc = updated
	}
	a.pool.Reload()
	view := toView(acc)
	if a.hub != nil {
		a.hub.Publish(Event{Type: "quota", Data: view})
	}
	writeJSON(w, 200, map[string]any{"ok": true, "account": view})
}
