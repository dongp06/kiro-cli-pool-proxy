package proxy

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/auth"
	"kiro-cli-pool-proxy/config"
	"log"
	"net/http"
	"os"
	"strings"
)

// Anthropic Messages API <-> Kiro GenerateAssistantResponse translation.
//
// Lets Anthropic-native clients (Claude Code CLI, opencode) run through the
// Kiro pool: the proxy accepts POST /v1/messages, translates to Kiro's
// conversationState, dispatches via the pool (credential swap + rotation), then
// translates the AWS event-stream response back into Anthropic SSE (or a single
// JSON body when stream=false).

// ---- Anthropic request types ----

type anthRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    json.RawMessage  `json:"system"` // string OR [{type:text,text}]
	Messages  []anthMessage    `json:"messages"`
	Tools     []anthTool       `json:"tools"`
	Stream    bool             `json:"stream"`
}

type anthMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string OR []block
}

type anthTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text"`
	// tool_use
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"` // string OR []block
	IsError   bool            `json:"is_error"`
}

// serveAnthropic handles POST /v1/messages.
func (s *Server) serveAnthropic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": "method not allowed"}})
		return
	}

	// API key: Anthropic clients send x-api-key; also accept Authorization: Bearer.
	key := strings.TrimSpace(r.Header.Get("x-api-key"))
	if key == "" {
		key = strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}
	apiKeyID, ok := s.cfg.ValidateAPIKey(key)
	if !ok {
		writeJSON(w, 401, anthErr("authentication_error", "invalid or missing API key"))
		return
	}
	if s.cfg.APIKeyOverCredit(apiKeyID) {
		writeJSON(w, 402, anthErr("invalid_request_error", "API key credit limit reached"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024))
	if err != nil {
		writeJSON(w, 400, anthErr("invalid_request_error", "read body"))
		return
	}
	r.Body.Close()

	var req anthRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 400, anthErr("invalid_request_error", "invalid JSON: "+err.Error()))
		return
	}

	kiroBody, err := buildKiroBody(&req)
	if err != nil {
		writeJSON(w, 400, anthErr("invalid_request_error", err.Error()))
		return
	}

	// Dispatch to the pool (credential swap + rotation + retry).
	resp, account, err := s.dispatchKiro(r, kiroBody)
	if err != nil {
		writeJSON(w, 502, anthErr("api_error", err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		writeJSON(w, resp.StatusCode, anthErr("api_error", truncate(string(b), 400)))
		return
	}

	if req.Stream {
		s.streamAnthropic(w, resp, &req, account, apiKeyID)
	} else {
		s.aggregateAnthropic(w, resp, &req, account, apiKeyID)
	}
}

func anthErr(typ, msg string) map[string]any {
	return map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": msg}}
}

// serveAnthropicCountTokens answers POST /v1/messages/count_tokens with an
// estimated input token count (Claude Code / AI-SDK may call this before a turn).
func (s *Server) serveAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024))
	r.Body.Close()
	var req anthRequest
	_ = json.Unmarshal(body, &req)
	writeJSON(w, 200, map[string]any{"input_tokens": estTokens(&req)})
}

// ---- Request translation: Anthropic -> Kiro ----

func buildKiroBody(req *anthRequest) ([]byte, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages required")
	}
	modelID := mapKiroModel(req.Model)
	systemText := extractSystem(req.System)

	// Split into history (all but last) + currentMessage (last).
	histMsgs := req.Messages
	var lastMsg anthMessage
	if len(histMsgs) > 0 {
		lastMsg = histMsgs[len(histMsgs)-1]
		histMsgs = histMsgs[:len(histMsgs)-1]
	}

	history := make([]map[string]any, 0, len(histMsgs))
	systemInjected := false
	for _, m := range histMsgs {
		blocks := parseBlocks(m.Content)
		switch m.Role {
		case "user":
			uim := userInputMessage(blocks, modelID)
			if !systemInjected && systemText != "" {
				uim["content"] = joinSystem(systemText, asString(uim["content"]))
				systemInjected = true
			}
			history = append(history, map[string]any{"userInputMessage": uim})
		case "assistant":
			history = append(history, map[string]any{"assistantResponseMessage": assistantResponseMessage(blocks)})
		}
	}

	// currentMessage from the last message (expected role=user).
	curBlocks := parseBlocks(lastMsg.Content)
	curUIM := userInputMessage(curBlocks, modelID)
	if !systemInjected && systemText != "" {
		curUIM["content"] = joinSystem(systemText, asString(curUIM["content"]))
		systemInjected = true
	}
	// Attach tools to the current message context.
	if len(req.Tools) > 0 {
		ctx, _ := curUIM["userInputMessageContext"].(map[string]any)
		if ctx == nil {
			ctx = map[string]any{}
			curUIM["userInputMessageContext"] = ctx
		}
		ctx["tools"] = kiroTools(req.Tools)
	}
	if lastMsg.Role == "assistant" {
		// Unusual: last message is assistant. Push it to history and use empty current.
		history = append(history, map[string]any{"assistantResponseMessage": assistantResponseMessage(curBlocks)})
		curUIM = userInputMessage(nil, modelID)
		if len(req.Tools) > 0 {
			curUIM["userInputMessageContext"] = map[string]any{"tools": kiroTools(req.Tools)}
		}
	}

	convState := map[string]any{
		"conversationId": newUUID(),
		"history":        history,
		"currentMessage": map[string]any{"userInputMessage": curUIM},
		"chatTriggerType": "MANUAL",
		"agentTaskType":   "vibe",
	}
	top := map[string]any{
		"profileArn":        "PLACEHOLDER", // dispatchKiro rewrites per account
		"conversationState": convState,
	}
	return json.Marshal(top)
}

// userInputMessage builds a Kiro userInputMessage from Anthropic content blocks.
func userInputMessage(blocks []anthBlock, modelID string) map[string]any {
	var text strings.Builder
	var toolResults []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_result":
			status := "success"
			if b.IsError {
				status = "error"
			}
			toolResults = append(toolResults, map[string]any{
				"toolUseId": b.ToolUseID,
				"status":    status,
				"content":   []map[string]any{{"text": extractResultText(b.Content)}},
			})
		}
	}
	ctx := map[string]any{}
	if len(toolResults) > 0 {
		ctx["toolResults"] = toolResults
	}
	uim := map[string]any{
		"content":                 text.String(),
		"origin":                  "KIRO_CLI",
		"modelId":                 modelID,
		"userInputMessageContext": ctx,
	}
	return uim
}

// assistantResponseMessage builds a Kiro assistantResponseMessage.
func assistantResponseMessage(blocks []anthBlock) map[string]any {
	var text strings.Builder
	var toolUses []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			var input any
			if len(b.Input) > 0 {
				_ = json.Unmarshal(b.Input, &input)
			}
			if input == nil {
				input = map[string]any{}
			}
			toolUses = append(toolUses, map[string]any{
				"toolUseId": b.ID,
				"name":      b.Name,
				"input":     input,
			})
		}
	}
	arm := map[string]any{"content": text.String()}
	if len(toolUses) > 0 {
		arm["toolUses"] = toolUses
	}
	return arm
}

func kiroTools(tools []anthTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		var schema any
		if len(t.InputSchema) > 0 {
			_ = json.Unmarshal(t.InputSchema, &schema)
		}
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		out = append(out, map[string]any{
			"toolSpecification": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": map[string]any{"json": schema},
			},
		})
	}
	return out
}

// parseBlocks normalizes Anthropic content (string OR []block) into blocks.
func parseBlocks(raw json.RawMessage) []anthBlock {
	if len(raw) == 0 {
		return nil
	}
	// string form
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []anthBlock{{Type: "text", Text: s}}
	}
	var blocks []anthBlock
	if json.Unmarshal(raw, &blocks) == nil {
		return blocks
	}
	return nil
}

func extractSystem(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []anthBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			sb.WriteString(b.Text)
		}
		return sb.String()
	}
	return ""
}

// extractResultText flattens tool_result content (string OR []block) to text.
func extractResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []anthBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" || b.Text != "" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	// object/array fallback: raw JSON as text
	return string(raw)
}

func joinSystem(system, content string) string {
	if content == "" {
		return system
	}
	return system + "\n\n" + content
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// kiroModels is the set of modelIds the Kiro backend accepts (from
// ListAvailableModels). Requests naming one of these pass through verbatim.
var kiroModels = map[string]bool{
	"auto": true, "claude-sonnet-5": true,
	"claude-opus-4.8": true, "claude-opus-4.7": true, "claude-opus-4.6": true, "claude-opus-4.5": true,
	"claude-sonnet-4.6": true, "claude-sonnet-4.5": true, "claude-sonnet-4": true,
	"claude-haiku-4.5": true,
	"gpt-5.6-sol": true, "gpt-5.6-terra": true, "gpt-5.6-luna": true,
	"deepseek-3.2": true, "minimax-m2.5": true, "minimax-m2.1": true,
	"glm-5": true, "qwen3-coder-next": true,
}

// mapKiroModel resolves an incoming (Anthropic/OpenAI) model id to a Kiro
// modelId. Precedence: KPP_KIRO_MODEL env > exact Kiro id pass-through >
// family heuristic > "auto" (server picks).
func mapKiroModel(model string) string {
	if m := strings.TrimSpace(os.Getenv("KPP_KIRO_MODEL")); m != "" {
		return m
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "auto"
	}
	if kiroModels[model] {
		return model
	}
	lm := strings.ToLower(model)
	switch {
	case strings.Contains(lm, "opus"):
		return "claude-opus-4.8"
	case strings.Contains(lm, "haiku"):
		return "claude-haiku-4.5"
	case strings.Contains(lm, "sonnet"):
		return "claude-sonnet-4.5"
	case strings.Contains(lm, "deepseek"):
		return "deepseek-3.2"
	case strings.Contains(lm, "qwen"):
		return "qwen3-coder-next"
	case strings.Contains(lm, "glm"):
		return "glm-5"
	case strings.Contains(lm, "minimax"):
		return "minimax-m2.5"
	case strings.HasPrefix(lm, "gpt") || strings.HasPrefix(lm, "o1") || strings.HasPrefix(lm, "o3"):
		return "gpt-5.6-terra"
	}
	return "auto"
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func newMsgID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "msg_" + hex.EncodeToString(b)
}

// ---- Dispatch to Kiro upstream via pool ----

func (s *Server) dispatchKiro(r *http.Request, body []byte) (*http.Response, *config.Account, error) {
	excluded := make(map[string]bool)
	retryLimit := s.pool.RetryLimit()
	for attempt := 0; attempt < retryLimit; attempt++ {
		account := s.pool.GetNext(excluded)
		if account == nil {
			return nil, nil, fmt.Errorf("no available accounts in pool")
		}
		if auth.NeedsRefresh(account) {
			if err := auth.RefreshToken(account); err != nil {
				s.pool.RecordFailure(account.ID, 401, "refresh failed")
				excluded[account.ID] = true
				continue
			}
			s.cfg.UpdateAccountToken(account.ID, account.AccessToken, account.RefreshToken, account.ExpiresAt)
		}

		newBody := RewriteProfileArn(body, account.ProfileArn)
		region := RegionFromProfileArn(account.ProfileArn)
		if region == "" {
			region = account.Region
		}
		if region == "" {
			region = "us-east-1"
		}
		host := runtimeHostForRegion(region)
		url := "https://" + host + "/"

		upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, strings.NewReader(string(newBody)))
		if err != nil {
			return nil, nil, err
		}
		// Headers that the real kiro-cli sends (required to avoid 403).
		upReq.Header.Set("Content-Type", "application/x-amz-json-1.0")
		upReq.Header.Set("X-Amz-Target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
		upReq.Header.Set("User-Agent", kiroUserAgent)
		upReq.Header.Set("X-Amz-User-Agent", kiroXAmzUserAgent)
		upReq.Header.Set("X-Amzn-Codewhisperer-Optout", "false")
		upReq.Header.Set("Amz-Sdk-Invocation-Id", newUUID())
		upReq.Header.Set("Amz-Sdk-Request", "attempt=1; max=3")
		upReq.Header.Set("Accept", "*/*")
		upReq.Header.Set("Authorization", "Bearer "+account.AccessToken)
		upReq.Header.Set("Host", host)
		upReq.Host = host
		if tt := TokenTypeHeader(account.AuthMethod); tt != "" {
			upReq.Header.Set("tokentype", tt)
		}

		resp, err := s.client.Do(upReq)
		if err != nil {
			s.pool.RecordFailure(account.ID, 0, err.Error())
			excluded[account.ID] = true
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode >= 500 {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			log.Printf("[anthropic] upstream %d (account %s): %s", resp.StatusCode, account.ID, truncate(string(b), 200))
			s.pool.RecordFailure(account.ID, resp.StatusCode, string(b))
			excluded[account.ID] = true
			continue
		}
		s.pool.RecordSuccess(account.ID)
		return resp, account, nil
	}
	return nil, nil, fmt.Errorf("all accounts exhausted after retries")
}

const (
	kiroUserAgent     = "aws-sdk-rust/1.3.15 ua/2.1 api/codewhispererstreaming/0.1.17593 os/linux lang/rust/1.92.0 md/appVersion-2.10.0 app/AmazonQ-For-CLI"
	kiroXAmzUserAgent = "aws-sdk-rust/1.3.15 ua/2.1 api/codewhispererstreaming/0.1.17593 os/linux lang/rust/1.92.0 m/F app/AmazonQ-For-CLI"
)

// ---- Response translation: Kiro event-stream -> Anthropic ----

// kiroFrameReader iterates AWS event-stream frames from r, calling cb per frame.
func kiroFrameReader(r io.Reader, cb func(eventType string, payload []byte)) {
	var buf []byte
	tmp := make([]byte, 32*1024)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for len(buf) >= esPreludeLen {
				total := binary.BigEndian.Uint32(buf[0:4])
				headersLen := binary.BigEndian.Uint32(buf[4:8])
				if total < esMinMsgBytes || total > esMaxMsgBytes || headersLen > esMaxHeaderLen {
					return // corrupt / not event-stream
				}
				if uint32(len(buf)) < total {
					break
				}
				headers := buf[esPreludeLen : esPreludeLen+headersLen]
				payload := buf[esPreludeLen+headersLen : total-esMsgCRCLen]
				cb(eventTypeFromHeaders(headers), append([]byte(nil), payload...))
				buf = buf[total:]
			}
		}
		if err != nil {
			return
		}
	}
}

type toolAccum struct {
	id    string
	name  string
	input strings.Builder
}

// parseAssistantText extracts the text delta from assistantResponseEvent.
func parseAssistantText(payload []byte) string {
	var o struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(payload, &o)
	return o.Content
}

// parseToolUse extracts fields from a toolUseEvent (best-effort; verified later).
func parseToolUse(payload []byte) (id, name, inputFragment string, stop bool) {
	var o struct {
		ToolUseID string          `json:"toolUseId"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Stop      bool            `json:"stop"`
	}
	_ = json.Unmarshal(payload, &o)
	id, name, stop = o.ToolUseID, o.Name, o.Stop
	if len(o.Input) > 0 {
		var s string
		if json.Unmarshal(o.Input, &s) == nil {
			inputFragment = s // input sent as JSON-string fragment
		} else {
			inputFragment = string(o.Input) // input sent as object/array
		}
	}
	return
}

func mapStopReason(kiro string) string {
	switch strings.ToUpper(kiro) {
	case "TOOL_USE":
		return "tool_use"
	case "MAX_TOKENS":
		return "max_tokens"
	case "END_TURN", "":
		return "end_turn"
	default:
		return "end_turn"
	}
}

// streamAnthropic translates the Kiro event-stream into Anthropic SSE.
func (s *Server) streamAnthropic(w http.ResponseWriter, resp *http.Response, req *anthRequest, account *config.Account, apiKeyID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	msgID := newMsgID()
	sse := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	sse("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": msgID, "type": "message", "role": "assistant",
			"model": req.Model, "content": []any{},
			"stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": estTokens(req), "output_tokens": 0},
		},
	})

	nextIndex := 0
	openIndex := -1
	openType := ""
	curToolID := ""
	var textOut strings.Builder
	stopReason := "END_TURN"
	var usage float64
	var contextPct float64

	closeOpen := func() {
		if openIndex >= 0 {
			sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": openIndex})
			openIndex = -1
			openType = ""
		}
	}

	kiroFrameReader(resp.Body, func(et string, payload []byte) {
		switch et {
		case "assistantResponseEvent":
			txt := parseAssistantText(payload)
			if txt == "" {
				return
			}
			if openType != "text" {
				closeOpen()
				openIndex = nextIndex
				nextIndex++
				openType = "text"
				sse("content_block_start", map[string]any{
					"type": "content_block_start", "index": openIndex,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
			}
			textOut.WriteString(txt)
			sse("content_block_delta", map[string]any{
				"type": "content_block_delta", "index": openIndex,
				"delta": map[string]any{"type": "text_delta", "text": txt},
			})
		case "toolUseEvent":
			id, name, frag, stop := parseToolUse(payload)
			if id != "" && id != curToolID {
				closeOpen()
				openIndex = nextIndex
				nextIndex++
				openType = "tool"
				curToolID = id
				sse("content_block_start", map[string]any{
					"type": "content_block_start", "index": openIndex,
					"content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}},
				})
			}
			if frag != "" && openType == "tool" {
				sse("content_block_delta", map[string]any{
					"type": "content_block_delta", "index": openIndex,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": frag},
				})
			}
			if stop {
				closeOpen()
				curToolID = ""
			}
		case "metadataEvent":
			var o struct {
				StopReason string `json:"stopReason"`
			}
			if json.Unmarshal(payload, &o) == nil && o.StopReason != "" {
				stopReason = o.StopReason
			}
		case "contextUsageEvent":
			var o struct {
				Pct float64 `json:"contextUsagePercentage"`
			}
			if json.Unmarshal(payload, &o) == nil {
				contextPct = o.Pct
			}
		case "meteringEvent":
			var o struct {
				Usage float64 `json:"usage"`
			}
			if json.Unmarshal(payload, &o) == nil {
				usage = o.Usage
			}
		}
	})

	closeOpen()
	sse("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": mapStopReason(stopReason), "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": estOutputTokens(textOut.Len())},
	})
	sse("message_stop", map[string]any{"type": "message_stop"})

	s.recordChatUsage(account, apiKeyID, usage, contextPct)
}

// aggregateAnthropic buffers the whole turn into a single Anthropic Messages JSON.
func (s *Server) aggregateAnthropic(w http.ResponseWriter, resp *http.Response, req *anthRequest, account *config.Account, apiKeyID string) {
	var textOut strings.Builder
	tools := map[string]*toolAccum{}
	var toolOrder []string
	stopReason := "END_TURN"
	var usage, contextPct float64

	kiroFrameReader(resp.Body, func(et string, payload []byte) {
		switch et {
		case "assistantResponseEvent":
			textOut.WriteString(parseAssistantText(payload))
		case "toolUseEvent":
			id, name, frag, _ := parseToolUse(payload)
			if id == "" {
				return
			}
			t, ok := tools[id]
			if !ok {
				t = &toolAccum{id: id, name: name}
				tools[id] = t
				toolOrder = append(toolOrder, id)
			}
			if name != "" {
				t.name = name
			}
			t.input.WriteString(frag)
		case "metadataEvent":
			var o struct {
				StopReason string `json:"stopReason"`
			}
			if json.Unmarshal(payload, &o) == nil && o.StopReason != "" {
				stopReason = o.StopReason
			}
		case "contextUsageEvent":
			var o struct {
				Pct float64 `json:"contextUsagePercentage"`
			}
			if json.Unmarshal(payload, &o) == nil {
				contextPct = o.Pct
			}
		case "meteringEvent":
			var o struct {
				Usage float64 `json:"usage"`
			}
			if json.Unmarshal(payload, &o) == nil {
				usage = o.Usage
			}
		}
	})

	content := []map[string]any{}
	if textOut.Len() > 0 {
		content = append(content, map[string]any{"type": "text", "text": textOut.String()})
	}
	for _, id := range toolOrder {
		t := tools[id]
		var input any
		if s := strings.TrimSpace(t.input.String()); s != "" {
			if json.Unmarshal([]byte(s), &input) != nil {
				input = map[string]any{}
			}
		} else {
			input = map[string]any{}
		}
		content = append(content, map[string]any{"type": "tool_use", "id": t.id, "name": t.name, "input": input})
	}

	out := map[string]any{
		"id": newMsgID(), "type": "message", "role": "assistant",
		"model": req.Model, "content": content,
		"stop_reason": mapStopReason(stopReason), "stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  estTokens(req),
			"output_tokens": estOutputTokens(textOut.Len()),
		},
	}
	writeJSON(w, 200, out)
	s.recordChatUsage(account, apiKeyID, usage, contextPct)
}

func (s *Server) recordChatUsage(account *config.Account, apiKeyID string, usage, contextPct float64) {
	s.cfg.RecordUsage(account.ID, usage)
	s.cfg.UpdateQuotaCurrentDelta(account.ID, 1)
	if apiKeyID != "" {
		s.cfg.RecordKeyUsage(apiKeyID, usage)
	}
	label := account.Email
	if label == "" {
		label = account.ID
	}
	log.Printf("[anthropic] OK account=%s credits=%.4f ctx=%.0f%%", label, usage, contextPct)
}

// estTokens/estOutputTokens are rough approximations (~4 chars/token); Kiro
// does not return token counts, but Anthropic clients expect a usage object.
func estTokens(req *anthRequest) int {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	n += len(req.System)
	if n < 4 {
		return 1
	}
	return n / 4
}

func estOutputTokens(textLen int) int {
	if textLen == 0 {
		return 1
	}
	return textLen/4 + 1
}
