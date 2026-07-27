package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"net/http"
	"strings"
)

// UsageLimitsResponse mirrors the GetUsageLimits management response.
// Shape confirmed from kiro-cli binary + Kiro-Go (verified 1:1).
type UsageLimitsResponse struct {
	UsageBreakdownList []usageBreakdown `json:"usageBreakdownList"`
	NextDateReset      json.Number      `json:"nextDateReset"`
	UserInfo           *usageUserInfo   `json:"userInfo"`
}

type usageBreakdown struct {
	ResourceType string  `json:"resourceType"`
	CurrentUsage float64 `json:"currentUsage"`
	UsageLimit   float64 `json:"usageLimit"`
	Unit         string  `json:"unit"`
}

// usageUserInfo carries the key/account owner identity returned by the
// management getUsageLimits endpoint when isEmailRequired=true (API-key probe).
type usageUserInfo struct {
	Email  string `json:"email"`
	UserId string `json:"userId"`
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
// For api_key (ksk_) accounts there is no profileArn: the region-bound GET form
// (management.{region}.kiro.dev/getUsageLimits?...) with TokenType: API_KEY is used.
func FetchUsageLimits(acc *config.Account) (*UsageLimitsResponse, error) {
	if acc.AuthMethod == "api_key" {
		return fetchUsageLimitsAPIKey(acc)
	}

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

// StartBackgroundRefresh and quota refresh are triggered on demand (admin panel
// "test"/"refresh quota" actions and per-request accounting) rather than by a
// background poller — the auto-check loop was removed in favor of SSE-pushed,
// action-driven updates.

// QuotaFromUsage extracts the chat-quota limit, current usage, and next reset
// timestamp from a GetUsageLimits response. Returns zeros when unavailable.
func QuotaFromUsage(resp *UsageLimitsResponse) (limit, current float64, nextResetUnix int64) {
	if resp == nil {
		return 0, 0, 0
	}
	limit, current, _ = pickAgenticBreakdown(resp)
	if resp.NextDateReset != "" {
		if ts, err := resp.NextDateReset.Int64(); err == nil {
			nextResetUnix = ts
		}
	}
	return limit, current, nextResetUnix
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
