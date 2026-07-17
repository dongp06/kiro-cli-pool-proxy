package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"log"
	"net/http"
	"strings"
	"time"
)

// UsageLimitsResponse mirrors the GetUsageLimits management response.
// Shape confirmed from kiro-cli binary + Kiro-Go (verified 1:1).
type UsageLimitsResponse struct {
	UsageBreakdownList []usageBreakdown `json:"usageBreakdownList"`
	NextDateReset      json.Number      `json:"nextDateReset"`
}

type usageBreakdown struct {
	ResourceType string  `json:"resourceType"`
	CurrentUsage float64 `json:"currentUsage"`
	UsageLimit   float64 `json:"usageLimit"`
	Unit         string  `json:"unit"`
}

// managementHost returns the control-plane host for a region (GetUsageLimits).
func managementHost(region string) string {
	switch region {
	case "", "us-east-1":
		return "management.us-east-1.kiro.dev"
	case "eu-central-1":
		return "management.eu-central-1.kiro.dev"
	case "us-gov-east-1":
		return "management.us-gov-east-1.kiro.dev"
	case "us-gov-west-1":
		return "management.us-gov-west-1.kiro.dev"
	default:
		return "management." + region + ".kiro.dev"
	}
}

// FetchUsageLimits calls GetUsageLimits for an account.
// Target: AmazonCodeWhispererService.GetUsageLimits, JSON POST, {origin, profileArn}.
func FetchUsageLimits(acc *config.Account) (*UsageLimitsResponse, error) {
	arn := strings.TrimSpace(acc.ProfileArn)
	if arn == "" {
		return nil, fmt.Errorf("profileArn required for GetUsageLimits")
	}

	// The control-plane region must match the PROFILE's region (from the ARN),
	// not the token's region — a token issued in eu-central-1 can own a profile
	// in us-east-1, and GetUsageLimits on the wrong region returns "Invalid token".
	region := regionFromArn(arn)
	if region == "" {
		region = acc.Region
	}
	if region == "" {
		region = "us-east-1"
	}

	host := managementHost(region)
	body, _ := json.Marshal(map[string]string{"origin": "KIRO_CLI", "profileArn": arn})

	req, err := http.NewRequest("POST", "https://"+host+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonCodeWhispererService.GetUsageLimits")
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	if tt := tokenTypeHeader(acc.AuthMethod); tt != "" {
		req.Header.Set("tokentype", tt)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result UsageLimitsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func regionFromArn(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ""
	}
	return parts[3]
}

func tokenTypeHeader(authMethod string) string {
	switch authMethod {
	case "social", "external_idp":
		return "EXTERNAL_IDP"
	case "api_key":
		return "API_KEY"
	default:
		return ""
	}
}

// StartQuotaPoller periodically refreshes each account's quota via GetUsageLimits.
// The AGENTIC_REQUEST breakdown drives quota-aware selection. Best-effort:
// failures are logged and never block the proxy.
func StartQuotaPoller(cfg *config.Config, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		// Initial poll shortly after startup.
		time.Sleep(3 * time.Second)
		pollAll(cfg)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			pollAll(cfg)
		}
	}()
}

func pollAll(cfg *config.Config) {
	for i := range cfg.Accounts {
		acc := &cfg.Accounts[i]
		if !acc.Enabled || acc.ProfileArn == "" {
			continue
		}
		resp, err := FetchUsageLimits(acc)
		if err != nil {
			log.Printf("[quota] %s (%s): %v", acc.ID, acc.Email, err)
			continue
		}
		limit, current, unit := pickAgenticBreakdown(resp)
		var nextReset int64
		if resp.NextDateReset != "" {
			if ts, err := resp.NextDateReset.Int64(); err == nil {
				nextReset = ts
			}
		}
		if limit > 0 {
			cfg.UpdateQuota(acc.ID, limit, current, nextReset)
			log.Printf("[quota] %s: %.2f/%.2f %s used (%.0f%%)",
				acc.Email, current, limit, unit, current/limit*100)
		}
	}
}

// pickAgenticBreakdown finds the AGENTIC_REQUEST breakdown (chat quota),
// falling back to the first breakdown entry.
func pickAgenticBreakdown(r *UsageLimitsResponse) (limit, current float64, unit string) {
	if r == nil || len(r.UsageBreakdownList) == 0 {
		return 0, 0, ""
	}
	// Live GetUsageLimits (KIRO POWER plan) returns resourceType="CREDIT" with
	// currentUsage/usageLimit as the chat request quota (e.g. 5998/10000).
	// "AGENTIC_REQUEST" is also a valid ResourceType enum value on other plans.
	// Prefer either, then fall back to the first breakdown entry.
	for _, want := range []string{"CREDIT", "AGENTIC_REQUEST"} {
		for _, b := range r.UsageBreakdownList {
			if strings.EqualFold(b.ResourceType, want) {
				return b.UsageLimit, b.CurrentUsage, b.Unit
			}
		}
	}
	b := r.UsageBreakdownList[0]
	return b.UsageLimit, b.CurrentUsage, b.Unit
}
