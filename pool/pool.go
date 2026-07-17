package pool

import (
	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Pool manages account selection with cooldown and rotation.
type Pool struct {
	cfg       *config.Config
	mu        sync.RWMutex
	index     atomic.Int64
	cooldowns map[string]time.Time // account ID -> cooldown expiry
}

// New creates a new account pool.
func New(cfg *config.Config) *Pool {
	return &Pool{
		cfg:       cfg,
		cooldowns: make(map[string]time.Time),
	}
}

// GetNext selects the next available account, excluding specified IDs.
func (p *Pool) GetNext(excluded map[string]bool) *config.Account {
	accounts := p.collectAvailable(excluded)
	if len(accounts) == 0 {
		return nil
	}

	if p.cfg.Strategy == "smart" {
		return pickMostRemainingQuota(accounts)
	}

	// Default: round-robin.
	idx := p.index.Add(1)
	return accounts[int(idx)%len(accounts)]
}

// pickMostRemainingQuota prefers the account with the largest remaining quota.
// Accounts without known quota are treated as having ample remaining budget so
// they are still used (optimistic). Ties fall back to least-recently-used.
func pickMostRemainingQuota(accounts []*config.Account) *config.Account {
	best := accounts[0]
	bestRemaining := remaining(best)
	for _, acc := range accounts[1:] {
		r := remaining(acc)
		if r > bestRemaining || (r == bestRemaining && acc.LastUsedUnix < best.LastUsedUnix) {
			best = acc
			bestRemaining = r
		}
	}
	return best
}

// remaining returns remaining quota; a large sentinel when quota is unknown.
func remaining(acc *config.Account) float64 {
	if acc.UsageLimit <= 0 {
		return 1e18 // unknown quota → treat as ample
	}
	r := acc.UsageLimit - acc.UsageCurrent
	if r < 0 {
		return 0
	}
	return r
}

// collectAvailable returns all accounts that are enabled, not cooling, and not excluded.
func (p *Pool) collectAvailable(excluded map[string]bool) []*config.Account {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := time.Now()
	var result []*config.Account

	for i := range p.cfg.Accounts {
		acc := &p.cfg.Accounts[i]
		if !acc.Enabled {
			continue
		}
		if excluded != nil && excluded[acc.ID] {
			continue
		}
		if cd, ok := p.cooldowns[acc.ID]; ok && now.Before(cd) {
			continue
		}
		if auth.IsExpired(acc) && acc.RefreshToken == "" {
			continue
		}
		// Skip accounts whose quota is exhausted (when quota info is known).
		if acc.UsageLimit > 0 && acc.UsageCurrent >= acc.UsageLimit {
			continue
		}
		result = append(result, acc)
	}
	return result
}

// RecordSuccess clears cooldown for an account.
func (p *Pool) RecordSuccess(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.cooldowns, id)
}

// Reload clears transient runtime state after the account list changes.
// Account data is read live from config, so this only resets cooldowns for
// accounts that no longer exist and is safe to call after add/remove/toggle.
func (p *Pool) Reload() {
	p.mu.Lock()
	defer p.mu.Unlock()
	valid := make(map[string]bool)
	for i := range p.cfg.Accounts {
		valid[p.cfg.Accounts[i].ID] = true
	}
	for id := range p.cooldowns {
		if !valid[id] {
			delete(p.cooldowns, id)
		}
	}
}

// RecordFailure applies cooldown based on HTTP status.
func (p *Pool) RecordFailure(id string, statusCode int, errText string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	duration := p.classifyCooldown(statusCode, errText)
	p.cooldowns[id] = time.Now().Add(duration)
	log.Printf("[pool] cooldown %s for %v (status=%d)", id, duration, statusCode)
}

// classifyCooldown determines cooldown duration based on error type.
func (p *Pool) classifyCooldown(status int, errText string) time.Duration {
	switch {
	case status == 429:
		return 30 * time.Second // rate limited
	case status == 402 || contains(errText, "quota"):
		return 15 * time.Minute // quota exhausted
	case status == 401 || status == 403:
		return 2 * time.Minute // auth error
	case status >= 500:
		return 10 * time.Second // server error, retry quickly
	default:
		return 30 * time.Second
	}
}

// AvailableCount returns number of currently available accounts.
func (p *Pool) AvailableCount() int {
	return len(p.collectAvailable(nil))
}

// RetryLimit returns max attempts for a request.
func (p *Pool) RetryLimit() int {
	n := p.AvailableCount()
	if n < 3 {
		return 3
	}
	if n > 10 {
		return 10
	}
	return n
}

func contains(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0 && containsCI(s, sub)
}

func containsCI(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			c1 := s[i+j]
			c2 := sub[j]
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 32
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 32
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
