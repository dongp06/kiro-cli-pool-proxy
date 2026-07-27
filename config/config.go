package config

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

func timeNowUnix() int64 { return time.Now().Unix() }

// GenerateMachineId returns a random UUID v4 string used as a per-account
// machine identifier for Kiro auth flows.
func GenerateMachineId() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback to a time-derived value; extremely unlikely to be hit.
		return fmt.Sprintf("%016x-%016x", time.Now().UnixNano(), time.Now().Unix())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// GetProxyURL returns the outbound HTTP proxy URL used by auth flows.
// The pool proxy does not currently route auth traffic through an upstream
// proxy, so this returns an empty string (direct connection).
func GetProxyURL() string { return "" }

// Account represents a Kiro API account with authentication credentials.
type Account struct {
	ID           string `json:"id"`
	Email        string `json:"email,omitempty"`
	UserId       string `json:"userId,omitempty"`   // Kiro/IdP user identifier
	Provider     string `json:"provider,omitempty"` // "BuilderId" | "AzureAD" | ...
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AuthMethod   string `json:"authMethod"` // "idc" | "social" | "external_idp" | "api_key"
	Region       string `json:"region,omitempty"`
	ProfileArn   string `json:"profileArn,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	TokenEndpoint string `json:"tokenEndpoint,omitempty"` // External IdP
	IssuerURL    string `json:"issuerUrl,omitempty"`      // External IdP
	Scopes       string `json:"scopes,omitempty"`         // External IdP
	MachineId    string `json:"machineId,omitempty"`
	Enabled      bool   `json:"enabled"`

	// Usage accounting (updated by the proxy as requests flow through).
	Credits      float64 `json:"credits,omitempty"`      // cumulative credits consumed
	Requests     int64   `json:"requests,omitempty"`     // total requests served
	LastUsedUnix int64   `json:"lastUsedUnix,omitempty"` // last successful use (unix sec)

	// Quota (populated from GetUsageLimits, optional).
	UsageLimit   float64 `json:"usageLimit,omitempty"`   // total credit limit
	UsageCurrent float64 `json:"usageCurrent,omitempty"` // current usage from upstream
	NextResetUnix int64  `json:"nextResetUnix,omitempty"`// next quota reset (unix sec)
}

// Config holds the proxy configuration.
type Config struct {
	ListenAddr string    `json:"listenAddr"`
	Strategy   string    `json:"strategy"` // "round-robin" | "smart"

	// ProxyAuth protects the proxy when it runs on a remote machine.
	// Clients send Authorization? No — the CLI's Authorization is the Kiro token.
	// Instead an optional shared secret header (X-Pool-Key) guards the proxy.
	PoolKey string `json:"poolKey,omitempty"`

	// AdminPassword protects the /admin web panel. Empty = no auth (localhost only).
	AdminPassword string `json:"adminPassword,omitempty"`

	// API key is mandatory. Clients present a key as the Bearer token; the proxy
	// validates it, then swaps in the pool account's real token.
	ApiKeys []APIKey `json:"apiKeys,omitempty"`

	// KiroModels is the set of modelIds the Kiro backend accepts, synced from
	// ListAvailableModels via the admin panel. Requests naming one of these are
	// passed through verbatim; empty falls back to the built-in default set.
	KiroModels []string `json:"kiroModels,omitempty"`

	Accounts []Account `json:"accounts"`

	mu   sync.RWMutex
	path string
}

var (
	global *Config
	once   sync.Once
)

// Load reads configuration from the given JSON file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{path: path}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:5000"
	}
	if cfg.Strategy == "" {
		cfg.Strategy = "round-robin"
	}

	global = cfg
	return cfg, nil
}

// Get returns the global config instance.
func Get() *Config {
	return global
}

// Save writes the current configuration back to disk.
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0600)
}

// GetAccounts returns a copy of enabled accounts.
func (c *Config) GetAccounts() []Account {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []Account
	for _, acc := range c.Accounts {
		if acc.Enabled {
			result = append(result, acc)
		}
	}
	return result
}

// GetAccountByID returns a copy of the account with the given ID.
func (c *Config) GetAccountByID(id string) (Account, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			return c.Accounts[i], true
		}
	}
	return Account{}, false
}

// SetAccountProfileArn stores the resolved profile ARN for an account by ID.
func (c *Config) SetAccountProfileArn(id, arn string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts[i].ProfileArn = arn
			break
		}
	}
}

// UpdateAccountToken updates the token for an account by ID.
func (c *Config) UpdateAccountToken(id, accessToken, refreshToken string, expiresAt int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts[i].AccessToken = accessToken
			if refreshToken != "" {
				c.Accounts[i].RefreshToken = refreshToken
			}
			c.Accounts[i].ExpiresAt = expiresAt
			break
		}
	}
}

// DisableAccount disables an account by ID.
func (c *Config) DisableAccount(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts[i].Enabled = false
			break
		}
	}
}

// RecordUsage adds credits/requests to an account after a successful turn.
func (c *Config) RecordUsage(id string, credits float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts[i].Credits += credits
			c.Accounts[i].Requests++
			c.Accounts[i].LastUsedUnix = timeNowUnix()
			break
		}
	}
}

// UpdateQuota stores quota snapshot from GetUsageLimits.
func (c *Config) UpdateQuota(id string, limit, current float64, nextResetUnix int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts[i].UsageLimit = limit
			c.Accounts[i].UsageCurrent = current
			c.Accounts[i].NextResetUnix = nextResetUnix
			break
		}
	}
}

// UpdateQuotaCurrentDelta increments the locally-tracked current usage between
// GetUsageLimits polls, so quota-aware selection stays fresh within a period.
func (c *Config) UpdateQuotaCurrentDelta(id string, delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			if c.Accounts[i].UsageLimit > 0 {
				c.Accounts[i].UsageCurrent += delta
			}
			break
		}
	}
}

// UsageSnapshot returns a read-only copy of accounting fields for reporting.
func (c *Config) UsageSnapshot() []Account {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Account, len(c.Accounts))
	copy(out, c.Accounts)
	return out
}

// AddAccount appends a new account (or replaces one with the same ID).
func (c *Config) AddAccount(acc Account) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Accounts {
		if c.Accounts[i].ID == acc.ID {
			c.Accounts[i] = acc
			return
		}
	}
	c.Accounts = append(c.Accounts, acc)
}

// RemoveAccount deletes an account by ID. Returns true if removed.
func (c *Config) RemoveAccount(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return true
		}
	}
	return false
}

// SetAccountEnabled toggles an account's enabled flag.
func (c *Config) SetAccountEnabled(id string, enabled bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Accounts {
		if c.Accounts[i].ID == id {
			c.Accounts[i].Enabled = enabled
			return true
		}
	}
	return false
}

// GetStrategy returns the current selection strategy.
func (c *Config) GetStrategy() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Strategy
}

// SetStrategy updates the selection strategy.
func (c *Config) SetStrategy(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Strategy = s
}

// GetListenAddr returns the configured listen address.
func (c *Config) GetListenAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ListenAddr
}

// GetAdminPassword returns the admin panel password (empty = no auth).
func (c *Config) GetAdminPassword() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AdminPassword
}

// GetKiroModels returns a copy of the synced Kiro model id list (may be empty).
func (c *Config) GetKiroModels() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.KiroModels))
	copy(out, c.KiroModels)
	return out
}

// SetKiroModels replaces the synced Kiro model id list.
func (c *Config) SetKiroModels(models []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.KiroModels = append([]string(nil), models...)
}

// APIKey is a client credential presented as the Bearer token.
type APIKey struct {
	ID           string  `json:"id"`
	Name         string  `json:"name,omitempty"`
	Key          string  `json:"key"`
	Enabled      bool    `json:"enabled"`
	CreditLimit  float64 `json:"creditLimit,omitempty"` // 0 = unlimited
	Requests     int64   `json:"requests,omitempty"`
	Credits      float64 `json:"credits,omitempty"`
	CreatedUnix  int64   `json:"createdUnix,omitempty"`
	LastUsedUnix int64   `json:"lastUsedUnix,omitempty"`
}

// APIKeyOverCredit reports whether a key has reached its credit limit.
func (c *Config) APIKeyOverCredit(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.ApiKeys {
		k := &c.ApiKeys[i]
		if k.ID == id {
			return k.CreditLimit > 0 && k.Credits >= k.CreditLimit
		}
	}
	return false
}

// GetAPIKey returns a copy of the key by ID.
func (c *Config) GetAPIKey(id string) (APIKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.ApiKeys {
		if c.ApiKeys[i].ID == id {
			return c.ApiKeys[i], true
		}
	}
	return APIKey{}, false
}

// APIKeysSnapshot returns a copy of all API keys.
func (c *Config) APIKeysSnapshot() []APIKey {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]APIKey, len(c.ApiKeys))
	copy(out, c.ApiKeys)
	return out
}

// AddAPIKey appends a new key (or replaces one with the same ID).
func (c *Config) AddAPIKey(k APIKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.ApiKeys {
		if c.ApiKeys[i].ID == k.ID {
			c.ApiKeys[i] = k
			return
		}
	}
	c.ApiKeys = append(c.ApiKeys, k)
}

// RemoveAPIKey deletes a key by ID. Returns true if removed.
func (c *Config) RemoveAPIKey(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.ApiKeys {
		if c.ApiKeys[i].ID == id {
			c.ApiKeys = append(c.ApiKeys[:i], c.ApiKeys[i+1:]...)
			return true
		}
	}
	return false
}

// SetAPIKeyEnabled toggles a key's enabled flag.
func (c *Config) SetAPIKeyEnabled(id string, enabled bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.ApiKeys {
		if c.ApiKeys[i].ID == id {
			c.ApiKeys[i].Enabled = enabled
			return true
		}
	}
	return false
}

// ValidateAPIKey returns the matching enabled key's ID (constant-time compare).
func (c *Config) ValidateAPIKey(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.ApiKeys {
		k := &c.ApiKeys[i]
		if k.Enabled && subtle.ConstantTimeCompare([]byte(k.Key), []byte(key)) == 1 {
			return k.ID, true
		}
	}
	return "", false
}

// RecordKeyUsage adds credits/requests to an API key after a successful turn.
func (c *Config) RecordKeyUsage(id string, credits float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.ApiKeys {
		if c.ApiKeys[i].ID == id {
			c.ApiKeys[i].Credits += credits
			c.ApiKeys[i].Requests++
			c.ApiKeys[i].LastUsedUnix = timeNowUnix()
			break
		}
	}
}
