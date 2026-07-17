package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

func timeNowUnix() int64 { return time.Now().Unix() }

// Account represents a Kiro API account with authentication credentials.
type Account struct {
	ID           string `json:"id"`
	Email        string `json:"email,omitempty"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AuthMethod   string `json:"authMethod"` // "idc" | "social" | "external_idp" | "api_key"
	Region       string `json:"region,omitempty"`
	ProfileArn   string `json:"profileArn,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"`
	TokenEndpoint string `json:"tokenEndpoint,omitempty"` // External IdP
	Scopes       string `json:"scopes,omitempty"`         // External IdP
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
		cfg.ListenAddr = "127.0.0.1:9999"
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
