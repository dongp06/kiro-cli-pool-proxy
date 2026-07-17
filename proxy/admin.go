package proxy

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"kiro-cli-pool-proxy/config"
	"kiro-cli-pool-proxy/kirolocal"
	"kiro-cli-pool-proxy/pool"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AdminHandler serves the web admin panel (API + embedded SPA).
type AdminHandler struct {
	cfg  *config.Config
	pool *pool.Pool

	mu       sync.Mutex
	sessions map[string]time.Time // token -> expiry
}

// NewAdminHandler creates the admin panel handler.
func NewAdminHandler(cfg *config.Config, p *pool.Pool) *AdminHandler {
	a := &AdminHandler{cfg: cfg, pool: p, sessions: make(map[string]time.Time)}
	go a.gcSessions()
	return a
}

const adminCookie = "kpp_admin"

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
	path := r.URL.Path

	// API routes
	if strings.HasPrefix(path, "/admin/api/") {
		a.serveAPI(w, r, strings.TrimPrefix(path, "/admin/api/"))
		return
	}

	// SPA (any /admin or /admin/... path serves the app)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminHTML))
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

	case strings.HasPrefix(route, "accounts/") && r.Method == http.MethodPatch:
		a.apiToggleAccount(w, r, strings.TrimPrefix(route, "accounts/"))

	case strings.HasPrefix(route, "accounts/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(route, "accounts/")
		if a.cfg.RemoveAccount(id) {
			a.cfg.Save()
			a.pool.Reload()
			writeJSON(w, 200, map[string]bool{"ok": true})
		} else {
			writeJSON(w, 404, map[string]string{"error": "not found"})
		}

	case route == "settings" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]any{
			"strategy":   a.cfg.GetStrategy(),
			"listenAddr": a.cfg.GetListenAddr(),
		})

	case route == "settings" && r.Method == http.MethodPatch:
		var body struct {
			Strategy string `json:"strategy"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Strategy == "round-robin" || body.Strategy == "smart" {
			a.cfg.SetStrategy(body.Strategy)
			a.cfg.Save()
		}
		writeJSON(w, 200, map[string]string{"strategy": a.cfg.GetStrategy()})

	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

func (a *AdminHandler) apiLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	pw := a.cfg.GetAdminPassword()
	if pw == "" {
		// No password set — issue a session anyway.
		a.setSessionCookie(w, a.newSession())
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Password), []byte(pw)) != 1 {
		writeJSON(w, 401, map[string]string{"error": "wrong password"})
		return
	}
	a.setSessionCookie(w, a.newSession())
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *AdminHandler) setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    tok,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
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
}

func toView(a config.Account) accountView {
	return accountView{
		ID: a.ID, Email: a.Email, AuthMethod: a.AuthMethod, Region: a.Region,
		Enabled: a.Enabled, Credits: a.Credits, Requests: a.Requests,
		LastUsedUnix: a.LastUsedUnix, UsageLimit: a.UsageLimit, UsageCurrent: a.UsageCurrent,
		NextResetUnix: a.NextResetUnix, HasProfileArn: strings.TrimSpace(a.ProfileArn) != "",
		TokenExpires: a.ExpiresAt,
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
	writeJSON(w, 200, toView(*acc))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
