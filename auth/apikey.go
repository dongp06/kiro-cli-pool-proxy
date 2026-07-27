package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"net/http"
	"strings"
)

// KiroAPIKeyPrefix is the marker for Kiro-issued API keys (ksk_...).
const KiroAPIKeyPrefix = "ksk_"

// kiroAPIKeyProbeRegions is the ordered set of regions probed when a ksk_ key is
// imported without an explicit region. Kiro API keys are bound to the region
// they were minted in; calling the wrong region's control plane returns a 4xx.
// Probing management.{region}.kiro.dev/getUsageLimits with the key finds the
// region that actually owns it.
var kiroAPIKeyProbeRegions = []string{"us-east-1", "eu-central-1", "ap-southeast-1"}

// IsKiroAPIKey reports whether a credential string looks like a Kiro API key.
func IsKiroAPIKey(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), KiroAPIKeyPrefix)
}

// KiroAPIKeyProbeResult is the outcome of a successful region probe.
type KiroAPIKeyProbeResult struct {
	Region string
	Email  string
	UserId string
}

// ProbeKiroAPIKeyRegion tries each candidate region's management control plane
// with the given ksk_ key and returns the first region that accepts it. When
// preferred is non-empty (and not "auto") it is tried first, de-duplicated from
// the defaults. Returns an error only if every region rejects the key.
func ProbeKiroAPIKeyRegion(key, preferred string) (*KiroAPIKeyProbeResult, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("empty API key")
	}
	regions := kiroAPIKeyCandidateRegions(preferred)
	lastErr := "unknown"
	for _, region := range regions {
		res, err := checkKiroAPIKeyRegion(key, region)
		if err == nil {
			res.Region = region
			return res, nil
		}
		lastErr = err.Error()
	}
	return nil, fmt.Errorf("API key not valid in any probed region (%s): %s",
		strings.Join(regions, ", "), lastErr)
}

// kiroAPIKeyCandidateRegions returns the ordered, de-duplicated region list to
// probe. A non-empty, non-"auto" preferred region is placed first.
func kiroAPIKeyCandidateRegions(preferred string) []string {
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	seen := make(map[string]bool)
	var out []string
	add := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" || seen[r] {
			return
		}
		seen[r] = true
		out = append(out, r)
	}
	if preferred != "" && preferred != "auto" {
		add(preferred)
	}
	for _, r := range kiroAPIKeyProbeRegions {
		add(r)
	}
	return out
}

// checkKiroAPIKeyRegion performs single-region control-plane validation
// (GET management.{region}.kiro.dev/getUsageLimits). On HTTP 200 it returns the
// key's owner identity (email may be empty). Any non-200 or transport error is
// returned so the caller can move to the next region.
func checkKiroAPIKeyRegion(key, region string) (*KiroAPIKeyProbeResult, error) {
	parsed, err := getUsageLimitsAPIKey(key, region)
	if err != nil {
		return nil, err
	}
	res := &KiroAPIKeyProbeResult{}
	if parsed.UserInfo != nil {
		res.Email = parsed.UserInfo.Email
		res.UserId = parsed.UserInfo.UserId
	}
	return res, nil
}

// getUsageLimitsAPIKey issues the region-bound GET getUsageLimits form used for
// api_key (ksk_) credentials, which have no profileArn. Returns the parsed
// response on HTTP 200; a non-200 or transport error otherwise.
func getUsageLimitsAPIKey(key, region string) (*UsageLimitsResponse, error) {
	url := fmt.Sprintf(
		"https://%s/getUsageLimits?origin=AI_EDITOR&resourceType=AGENTIC_REQUEST&isEmailRequired=true",
		managementHost(region),
	)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	req.Header.Set("TokenType", "API_KEY")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxProfileResponseBytes))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result UsageLimitsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// fetchUsageLimitsAPIKey retrieves usage limits for a stored api_key account via
// its owning region (established at import time). No profileArn is needed.
func fetchUsageLimitsAPIKey(acc *config.Account) (*UsageLimitsResponse, error) {
	return getUsageLimitsAPIKey(acc.AccessToken, strings.TrimSpace(acc.Region))
}

// ImportKiroAPIKey validates a ksk_ key by probing for its owning region and
// returns a ready-to-store api_key account. The region probe doubles as the
// key's validity check, so a returned account is known-good.
func ImportKiroAPIKey(key, preferredRegion string) (*config.Account, error) {
	key = strings.TrimSpace(key)
	if !IsKiroAPIKey(key) {
		return nil, fmt.Errorf("not a Kiro API key (expected %s prefix)", KiroAPIKeyPrefix)
	}
	res, err := ProbeKiroAPIKeyRegion(key, preferredRegion)
	if err != nil {
		return nil, err
	}
	return &config.Account{
		Email:       res.Email,
		UserId:      res.UserId,
		AccessToken: key,
		AuthMethod:  "api_key",
		Region:      res.Region,
	}, nil
}
