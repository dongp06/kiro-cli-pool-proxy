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

const maxModelsResponseBytes = 1 << 20

// KiroModel is one model advertised by the Kiro backend's ListAvailableModels.
type KiroModel struct {
	ModelId   string `json:"modelId"`
	ModelName string `json:"modelName,omitempty"`
}

// ListAvailableModels calls the ListAvailableModels control-plane operation for
// an account and returns the advertised modelIds. Same protocol as
// ListAvailableProfiles: management.{region}.kiro.dev with AWS-JSON-1.0.
//
// For api_key accounts (no profileArn) the profileArn field is omitted; for
// token accounts it is required by the backend.
func ListAvailableModels(acc *config.Account) ([]KiroModel, error) {
	if acc == nil {
		return nil, fmt.Errorf("account is nil")
	}

	region := regionFromArn(acc.ProfileArn)
	if region == "" {
		region = strings.TrimSpace(strings.ToLower(acc.Region))
	}
	if !kiroRegionPattern.MatchString(region) {
		region = "us-east-1"
	}
	endpoint := "https://" + managementHost(region) + "/"

	reqBody := map[string]interface{}{"origin": "KIRO_CLI"}
	if arn := strings.TrimSpace(acc.ProfileArn); arn != "" {
		reqBody["profileArn"] = arn
	}
	payload, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "AmazonCodeWhispererService.ListAvailableModels")
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	if tt := tokenTypeHeader(acc.AuthMethod); tt != "" {
		req.Header.Set("tokentype", tt)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxProfileErrorBytes))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelsResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxModelsResponseBytes {
		return nil, fmt.Errorf("models response exceeds %d bytes", maxModelsResponseBytes)
	}

	var result struct {
		Models []struct {
			ModelId   string `json:"modelId"`
			ModelName string `json:"modelName"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	out := make([]KiroModel, 0, len(result.Models))
	seen := make(map[string]struct{})
	for _, m := range result.Models {
		id := strings.TrimSpace(m.ModelId)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, KiroModel{ModelId: id, ModelName: strings.TrimSpace(m.ModelName)})
	}
	return out, nil
}
