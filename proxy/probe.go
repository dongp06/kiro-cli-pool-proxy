package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"net/http"
	"strings"
	"time"
)

// probeClient is a dedicated client for admin "test model" probes. Separate
// from the proxy hot-path client so a slow probe never contends with chat.
var probeClient = &http.Client{
	Timeout: 60 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// probeModel issues a minimal single-turn GenerateAssistantResponse against the
// given account for a specific modelId and returns the assistant's text reply.
// Used by the admin "test model" action to verify an account can actually serve
// a named model.
func probeModel(acc *config.Account, modelID string) (string, error) {
	if acc == nil {
		return "", fmt.Errorf("account is nil")
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "auto"
	}

	isAPIKey := acc.AuthMethod == "api_key"
	uim := map[string]any{
		"content": "Reply with the single word: ok",
		"origin":  "KIRO_CLI",
		"modelId": modelID,
		"userInputMessageContext": map[string]any{},
	}
	convState := map[string]any{
		"conversationId":  newUUID(),
		"history":         []any{},
		"currentMessage":  map[string]any{"userInputMessage": uim},
		"chatTriggerType": "MANUAL",
		"agentTaskType":   "vibe",
	}
	top := map[string]any{"conversationState": convState}
	if !isAPIKey {
		top["profileArn"] = acc.ProfileArn
	}
	body, err := json.Marshal(top)
	if err != nil {
		return "", err
	}

	region := RegionFromProfileArn(acc.ProfileArn)
	if region == "" {
		region = acc.Region
	}
	if region == "" {
		region = "us-east-1"
	}
	host := runtimeHostForRegion(region)
	url := "https://" + host + "/"
	if isAPIKey {
		url = apiKeyChatURL(region)
		host = hostFromURL(url)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	if isAPIKey {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	}
	req.Header.Set("X-Amz-Target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
	req.Header.Set("User-Agent", kiroUserAgent)
	req.Header.Set("X-Amz-User-Agent", kiroXAmzUserAgent)
	req.Header.Set("X-Amzn-Codewhisperer-Optout", "false")
	req.Header.Set("Amz-Sdk-Invocation-Id", newUUID())
	req.Header.Set("Amz-Sdk-Request", "attempt=1; max=1")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Authorization", "Bearer "+acc.AccessToken)
	req.Header.Set("Host", host)
	req.Host = host
	if tt := TokenTypeHeader(acc.AuthMethod); tt != "" {
		req.Header.Set("tokentype", tt)
	}

	resp, err := probeClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(b), 300))
	}

	var reply strings.Builder
	kiroFrameReader(resp.Body, func(eventType string, payload []byte) {
		if eventType == "assistantResponseEvent" {
			reply.WriteString(parseAssistantText(payload))
		}
	})
	out := strings.TrimSpace(reply.String())
	if out == "" {
		return "", fmt.Errorf("empty response (model may be unavailable for this account)")
	}
	return truncate(out, 500), nil
}
