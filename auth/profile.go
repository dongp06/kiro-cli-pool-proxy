package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"net/http"
	"regexp"
	"strings"
)

const (
	maxProfileResponseBytes = 1 << 20
	maxProfileErrorBytes    = 64 << 10
)

// Kiro data-plane regions that host Amazon Q Developer profiles. Kept explicit
// so a persisted auth region can never turn into an arbitrary outbound host.
var defaultProfileRegions = []string{"us-east-1", "eu-central-1"}

var (
	kiroRegionPattern  = regexp.MustCompile(`^[a-z]{2}(?:-[a-z0-9]+)+-[0-9]+$`)
	kiroAccountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	kiroProfilePattern = regexp.MustCompile(`^[A-Za-z0-9+=,.@_-]+$`)
)

// KiroProfile is one selectable Kiro data-plane profile.
type KiroProfile struct {
	ARN    string
	Name   string
	Region string
}

// parseProfileArn strictly validates a Kiro profile ARN and returns its region.
// Format: arn:aws:codewhisperer:{region}:{account}:profile/{id}
func parseProfileArn(profileArn string) (canonical, region string, ok bool) {
	canonical = strings.TrimSpace(profileArn)
	parts := strings.SplitN(canonical, ":", 6)
	if len(parts) != 6 ||
		parts[0] != "arn" ||
		parts[1] != "aws" ||
		parts[2] != "codewhisperer" ||
		!kiroRegionPattern.MatchString(parts[3]) ||
		!kiroAccountPattern.MatchString(parts[4]) ||
		!strings.HasPrefix(parts[5], "profile/") {
		return "", "", false
	}
	profileID := strings.TrimPrefix(parts[5], "profile/")
	if !kiroProfilePattern.MatchString(profileID) {
		return "", "", false
	}
	return canonical, parts[3], true
}

// profileRegionCandidates returns the ordered, de-duplicated set of regions to
// probe for an account. The account's own ARN region (if any) comes first.
func profileRegionCandidates(acc *config.Account) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(defaultProfileRegions)+1)
	add := func(region string) {
		region = strings.TrimSpace(strings.ToLower(region))
		if !kiroRegionPattern.MatchString(region) {
			return
		}
		if _, ok := seen[region]; ok {
			return
		}
		seen[region] = struct{}{}
		out = append(out, region)
	}
	if acc != nil {
		add(regionFromArn(acc.ProfileArn))
		add(acc.Region)
	}
	for _, r := range defaultProfileRegions {
		add(r)
	}
	return out
}

// listProfilesInRegion calls ListAvailableProfiles for one region and returns
// all strictly-validated profiles, de-duplicated by ARN.
//
// ListAvailableProfiles is an AmazonCodeWhispererService control-plane
// operation, same as GetUsageLimits — it uses the management.{region}.kiro.dev
// host with the AWS-JSON-1.0 protocol (Content-Type application/x-amz-json-1.0 +
// X-Amz-Target), NOT the legacy REST + application/json path. The REST path
// returns HTTP 400 REQUEST_BODY_INVALID for current tokens.
func listProfilesInRegion(acc *config.Account, region string) ([]KiroProfile, error) {
	region = strings.TrimSpace(strings.ToLower(region))
	if !kiroRegionPattern.MatchString(region) {
		return nil, fmt.Errorf("invalid profile region %q", region)
	}
	endpoint := "https://" + managementHost(region) + "/"

	profiles := make([]KiroProfile, 0)
	seen := make(map[string]struct{})
	nextToken := ""
	const maxPages = 20
	const pageSize = 10

	for page := 0; page < maxPages; page++ {
		reqBody := map[string]interface{}{"maxResults": pageSize}
		if nextToken != "" {
			reqBody["nextToken"] = nextToken
		}
		payload, _ := json.Marshal(reqBody)

		req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("X-Amz-Target", "AmazonCodeWhispererService.ListAvailableProfiles")
		req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		if tt := tokenTypeHeader(acc.AuthMethod); tt != "" {
			req.Header.Set("tokentype", tt)
		}

		resp, err := httpClient().Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, maxProfileErrorBytes))
			resp.Body.Close()
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxProfileResponseBytes+1))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > maxProfileResponseBytes {
			return nil, fmt.Errorf("profile response exceeds %d bytes", maxProfileResponseBytes)
		}

		var result struct {
			Profiles []struct {
				ARN  string `json:"arn"`
				Name string `json:"profileName"`
			} `json:"profiles"`
			NextToken string `json:"nextToken"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		for _, p := range result.Profiles {
			arn, arnRegion, ok := parseProfileArn(p.ARN)
			if !ok {
				continue
			}
			if _, exists := seen[arn]; exists {
				continue
			}
			seen[arn] = struct{}{}
			profiles = append(profiles, KiroProfile{ARN: arn, Name: strings.TrimSpace(p.Name), Region: arnRegion})
		}
		nextToken = strings.TrimSpace(result.NextToken)
		if nextToken == "" {
			break
		}
	}
	return profiles, nil
}

// DiscoverProfiles returns every validated profile across the account's
// candidate regions, de-duplicated by ARN. A failed region never hides
// profiles found elsewhere.
func DiscoverProfiles(acc *config.Account) ([]KiroProfile, error) {
	if acc == nil {
		return nil, fmt.Errorf("account is nil")
	}
	profiles := make([]KiroProfile, 0)
	seen := make(map[string]struct{})
	var lastErr error
	for _, region := range profileRegionCandidates(acc) {
		found, err := listProfilesInRegion(acc, region)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", region, err)
			continue
		}
		for _, p := range found {
			if _, ok := seen[p.ARN]; ok {
				continue
			}
			seen[p.ARN] = struct{}{}
			profiles = append(profiles, p)
		}
	}
	if len(profiles) > 0 {
		return profiles, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no available Kiro profile")
}

// ResolveProfileArn discovers and returns the account's profile ARN. When the
// account already has a valid ARN it is returned unchanged. When multiple
// profiles exist the first is chosen (matching kiro-cli default behavior).
func ResolveProfileArn(acc *config.Account) (string, error) {
	if acc == nil {
		return "", fmt.Errorf("account is nil")
	}
	if _, _, ok := parseProfileArn(acc.ProfileArn); ok {
		return strings.TrimSpace(acc.ProfileArn), nil
	}
	profiles, err := DiscoverProfiles(acc)
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		return "", fmt.Errorf("no available Kiro profile")
	}
	return profiles[0].ARN, nil
}
