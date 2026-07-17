package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// Social refresh endpoint (Kiro desktop auth)
	socialRefreshURL = "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
	// Social User-Agent (matches kiro-cli capture)
	socialUserAgent = "KiroIDE-0.12.184-cbc292d871e4ce3f7bafe59d8ed202b7176266c0fe5beeca4f713058c8b40b1a"
	// Token refresh skew - refresh 5 minutes before expiry
	refreshSkew = 5 * time.Minute
)

// refreshResult holds the outcome of a token refresh.
type refreshResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

// inflight tracks in-progress refresh calls to avoid stampedes.
var (
	inflightMu sync.Mutex
	inflight   = make(map[string]*inflightEntry)
)

type inflightEntry struct {
	done   chan struct{}
	result *refreshResult
	err    error
}

// NeedsRefresh returns true if the account's token is expiring soon.
func NeedsRefresh(acc *config.Account) bool {
	if acc.AuthMethod == "api_key" {
		return false
	}
	if acc.RefreshToken == "" {
		return false
	}
	if acc.ExpiresAt == 0 {
		return true // no expiry info, always refresh
	}
	return time.Now().Add(refreshSkew).Unix() >= acc.ExpiresAt
}

// IsExpired returns true if the token is already expired.
func IsExpired(acc *config.Account) bool {
	if acc.AuthMethod == "api_key" {
		return false
	}
	if acc.ExpiresAt == 0 {
		return false
	}
	return time.Now().Unix() >= acc.ExpiresAt
}

// RefreshToken refreshes the access token for the given account.
// Uses in-flight deduplication to prevent stampedes.
func RefreshToken(acc *config.Account) error {
	key := acc.ID

	inflightMu.Lock()
	if entry, ok := inflight[key]; ok {
		inflightMu.Unlock()
		<-entry.done
		if entry.err != nil {
			return entry.err
		}
		applyResult(acc, entry.result)
		return nil
	}

	entry := &inflightEntry{done: make(chan struct{})}
	inflight[key] = entry
	inflightMu.Unlock()

	result, err := doRefresh(acc)
	entry.result = result
	entry.err = err
	close(entry.done)

	inflightMu.Lock()
	delete(inflight, key)
	inflightMu.Unlock()

	if err != nil {
		return err
	}
	applyResult(acc, result)
	return nil
}

func applyResult(acc *config.Account, r *refreshResult) {
	acc.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		acc.RefreshToken = r.RefreshToken
	}
	acc.ExpiresAt = r.ExpiresAt
}

func doRefresh(acc *config.Account) (*refreshResult, error) {
	switch {
	case acc.AuthMethod == "external_idp" || acc.TokenEndpoint != "":
		return refreshExternalIdp(acc)
	case acc.AuthMethod == "social":
		return refreshSocial(acc)
	default:
		// IdC / Builder ID
		return refreshOIDC(acc)
	}
}

// refreshOIDC refreshes via AWS IAM Identity Center OIDC.
func refreshOIDC(acc *config.Account) (*refreshResult, error) {
	region := acc.Region
	if region == "" {
		region = "us-east-1"
	}

	tokenURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)

	payload := map[string]string{
		"clientId":     acc.ClientID,
		"clientSecret": acc.ClientSecret,
		"refreshToken": acc.RefreshToken,
		"grantType":    "refresh_token",
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", tokenURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("oidc refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oidc decode response: %w", err)
	}

	expiresAt := time.Now().Unix() + result.ExpiresIn

	return &refreshResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// refreshSocial refreshes via Kiro desktop social auth endpoint.
func refreshSocial(acc *config.Account) (*refreshResult, error) {
	payload := map[string]string{
		"refreshToken": acc.RefreshToken,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", socialRefreshURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", socialUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("social refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("social refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("social decode response: %w", err)
	}

	expiresAt := time.Now().Unix() + result.ExpiresIn

	return &refreshResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// refreshExternalIdp refreshes via external identity provider token endpoint.
func refreshExternalIdp(acc *config.Account) (*refreshResult, error) {
	if acc.TokenEndpoint == "" {
		return nil, fmt.Errorf("external_idp: no tokenEndpoint configured")
	}

	data := url.Values{
		"client_id":     {acc.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {acc.RefreshToken},
	}
	if acc.Scopes != "" {
		data.Set("scope", acc.Scopes)
	}

	req, _ := http.NewRequest("POST", acc.TokenEndpoint, strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("external_idp refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("external_idp refresh failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("external_idp decode response: %w", err)
	}

	expiresAt := time.Now().Unix() + result.ExpiresIn

	return &refreshResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// StartBackgroundRefresh starts a goroutine that periodically refreshes tokens.
func StartBackgroundRefresh(cfg *config.Config) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			for i := range cfg.Accounts {
				acc := &cfg.Accounts[i]
				if !acc.Enabled || acc.AuthMethod == "api_key" {
					continue
				}
				if NeedsRefresh(acc) {
					if err := RefreshToken(acc); err != nil {
						log.Printf("[auth] refresh %s (%s): %v", acc.ID, acc.Email, err)
					} else {
						log.Printf("[auth] refreshed %s (%s), expires %d", acc.ID, acc.Email, acc.ExpiresAt)
						cfg.UpdateAccountToken(acc.ID, acc.AccessToken, acc.RefreshToken, acc.ExpiresAt)
						cfg.Save()
					}
				}
			}
		}
	}()
}
