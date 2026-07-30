package proxy

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"kiro-cli-pool-proxy/config"
	"net/http"
	"strings"
	"time"

	"crypto/rand"
)

// OpenAI Chat Completions API <-> Kiro GenerateAssistantResponse translation.
//
// Lets OpenAI-native clients (Codex CLI, opencode) run through the Kiro pool via
// POST /v1/chat/completions. Reuses dispatchKiro + kiroFrameReader from the
// Anthropic layer; only the request/response shapes differ.

// ---- OpenAI request types ----

type oaiRequest struct {
	Model         string    `json:"model"`
	Messages      []oaiMsg  `json:"messages"`
	Tools         []oaiTool `json:"tools"`
	Stream        bool      `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type oaiMsg struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string OR []part OR null
	ToolCalls  []oaiToolCall   `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// serveOpenAI handles POST /v1/chat/completions.
func (s *Server) serveOpenAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, oaiErr("invalid_request_error", "method not allowed"))
		return
	}
	key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("x-api-key"))
	}
	apiKeyID, ok := s.cfg.ValidateAPIKey(key)
	if !ok {
		writeJSON(w, 401, oaiErr("invalid_request_error", "invalid or missing API key"))
		return
	}
	if s.cfg.APIKeyOverCredit(apiKeyID) {
		writeJSON(w, 402, oaiErr("insufficient_quota", "API key credit limit reached"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024))
	if err != nil {
		writeJSON(w, 400, oaiErr("invalid_request_error", "read body"))
		return
	}
	r.Body.Close()

	var req oaiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 400, oaiErr("invalid_request_error", "invalid JSON: "+err.Error()))
		return
	}

	kiroBody, err := buildKiroBodyOpenAI(&req)
	if err != nil {
		writeJSON(w, 400, oaiErr("invalid_request_error", err.Error()))
		return
	}

	resp, account, err := s.dispatchKiro(r, kiroBody)
	if err != nil {
		writeJSON(w, 502, oaiErr("api_error", err.Error()))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		writeJSON(w, resp.StatusCode, oaiErr("api_error", truncate(string(b), 400)))
		return
	}
	s.maybeCapture("oc-openai", body, kiroBody, resp)

	if req.Stream {
		s.streamOpenAI(w, resp, &req, account, apiKeyID)
	} else {
		s.aggregateOpenAI(w, resp, &req, account, apiKeyID)
	}
}

func oaiErr(typ, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"type": typ, "message": msg, "code": nil, "param": nil}}
}

// serveOpenAIModels returns a static model list (some clients probe /v1/models).
func (s *Server) serveOpenAIModels(w http.ResponseWriter, r *http.Request) {
	created := time.Now().Unix()
	models := []string{"claude-3-5-sonnet", "claude-sonnet-4", "claude-opus-4", "gpt-4o", "kiro-auto"}
	data := make([]map[string]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{"id": m, "object": "model", "created": created, "owned_by": "kiro-pool"})
	}
	writeJSON(w, 200, map[string]any{"object": "list", "data": data})
}

// ---- Request translation: OpenAI -> Kiro ----

func buildKiroBodyOpenAI(req *oaiRequest) ([]byte, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages required")
	}
	modelID := mapKiroModel(req.Model)

	var history []map[string]any
	var systemText string
	var pendingContent strings.Builder
	var pendingToolResults []map[string]any
	systemInjected := false

	flushUser := func(isCurrent bool) map[string]any {
		content := pendingContent.String()
		if !systemInjected && systemText != "" {
			content = joinSystem(systemText, content)
			systemInjected = true
		}
		uim := map[string]any{
			"content": content,
			"origin":  "KIRO_CLI",
			"modelId": modelID,
		}
		ctx := map[string]any{}
		if len(pendingToolResults) > 0 {
			ctx["toolResults"] = pendingToolResults
		}
		uim["userInputMessageContext"] = ctx
		pendingContent.Reset()
		pendingToolResults = nil
		return uim
	}
	hasPending := func() bool {
		return pendingContent.Len() > 0 || len(pendingToolResults) > 0
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			if s := oaiContentText(m.Content); s != "" {
				if systemText != "" {
					systemText += "\n\n"
				}
				systemText += s
			}
		case "user":
			if pendingContent.Len() > 0 {
				pendingContent.WriteString("\n")
			}
			pendingContent.WriteString(oaiContentText(m.Content))
		case "tool":
			pendingToolResults = append(pendingToolResults, map[string]any{
				"toolUseId": m.ToolCallID,
				"status":    "success",
				"content":   []map[string]any{{"text": oaiContentText(m.Content)}},
			})
		case "assistant":
			// Flush any pending user/tool turn before the assistant turn.
			if hasPending() {
				history = append(history, map[string]any{"userInputMessage": flushUser(false)})
			}
			arm := map[string]any{"content": oaiContentText(m.Content)}
			if len(m.ToolCalls) > 0 {
				var tus []map[string]any
				for _, tc := range m.ToolCalls {
					var input any
					if strings.TrimSpace(tc.Function.Arguments) != "" {
						if json.Unmarshal([]byte(tc.Function.Arguments), &input) != nil {
							input = map[string]any{}
						}
					} else {
						input = map[string]any{}
					}
					tus = append(tus, map[string]any{"toolUseId": tc.ID, "name": tc.Function.Name, "input": input})
				}
				arm["toolUses"] = tus
			}
			history = append(history, map[string]any{"assistantResponseMessage": arm})
		}
	}

	// Remaining pending turn is the current message.
	curUIM := flushUser(true)
	if len(req.Tools) > 0 {
		ctx, _ := curUIM["userInputMessageContext"].(map[string]any)
		if ctx == nil {
			ctx = map[string]any{}
			curUIM["userInputMessageContext"] = ctx
		}
		ctx["tools"] = kiroToolsOpenAI(req.Tools)
	}

	convState := map[string]any{
		"conversationId":  newUUID(),
		"history":         history,
		"currentMessage":  map[string]any{"userInputMessage": curUIM},
		"chatTriggerType": "MANUAL",
		"agentTaskType":   "vibe",
	}
	return json.Marshal(map[string]any{
		"profileArn":        "PLACEHOLDER",
		"conversationState": convState,
	})
}

func kiroToolsOpenAI(tools []oaiTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		var schema any
		if len(t.Function.Parameters) > 0 {
			_ = json.Unmarshal(t.Function.Parameters, &schema)
		}
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		schema = normalizeToolSchema(schema)
		out = append(out, map[string]any{
			"toolSpecification": map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"inputSchema": map[string]any{"json": schema},
			},
		})
	}
	return out
}

// oaiContentText flattens OpenAI content (string OR []part) to text.
func oaiContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

func mapFinishReason(kiro string) string {
	switch strings.ToUpper(kiro) {
	case "TOOL_USE":
		return "tool_calls"
	case "MAX_TOKENS":
		return "length"
	default:
		return "stop"
	}
}

func newChatID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "chatcmpl-" + hex.EncodeToString(b)
}

// ---- Response translation: Kiro event-stream -> OpenAI ----

func (s *Server) streamOpenAI(w http.ResponseWriter, resp *http.Response, req *oaiRequest, account *config.Account, apiKeyID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher, _ := w.(http.Flusher)

	id := newChatID()
	created := time.Now().Unix()
	send := func(chunk map[string]any) {
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}

	// First chunk announces the assistant role.
	send(chunk(map[string]any{"role": "assistant"}, nil))

	stopReason := "END_TURN"
	var usage, contextPct float64
	var sawMetering bool
	var tokens TokenMeter
	toolIndex := map[string]int{}
	nextToolIdx := 0
	curToolID := ""
	outChars := 0

	kiroFrameReader(resp.Body, func(et string, payload []byte) {
		tokens.Observe(payload)
		switch et {
		case "reasoningContentEvent":
			var o struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(payload, &o) == nil && o.Text != "" {
				send(chunk(map[string]any{"reasoning_content": o.Text}, nil))
			}
		case "assistantResponseEvent":
			if txt := parseAssistantText(payload); txt != "" {
				outChars += len(txt)
				send(chunk(map[string]any{"content": txt}, nil))
			}
		case "toolUseEvent":
			tid, name, frag, stop := parseToolUse(payload)
			if tid != "" && tid != curToolID {
				curToolID = tid
				idx, ok := toolIndex[tid]
				if !ok {
					idx = nextToolIdx
					nextToolIdx++
					toolIndex[tid] = idx
				}
				send(chunk(map[string]any{"tool_calls": []map[string]any{{
					"index": idx, "id": tid, "type": "function",
					"function": map[string]any{"name": name, "arguments": ""},
				}}}, nil))
			}
			if frag != "" && curToolID != "" {
				send(chunk(map[string]any{"tool_calls": []map[string]any{{
					"index":    toolIndex[curToolID],
					"function": map[string]any{"arguments": frag},
				}}}, nil))
			}
			if stop {
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
			if n, ok := creditFromPayload(payload); ok {
				usage += n
				sawMetering = true
			}
		}
	})

	send(chunk(map[string]any{}, mapFinishReason(stopReason)))
	// stream_options.include_usage → final usage-only chunk (OpenAI convention).
	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		prompt := estOpenAITokens(req)
		comp := estOutputTokens(outChars)
		send(map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
			"choices": []any{},
			"usage":   map[string]any{"prompt_tokens": prompt, "completion_tokens": comp, "total_tokens": prompt + comp},
		})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
	s.recordChatUsage(account, apiKeyID, usage, contextPct, sawMetering, tokens)
}

func (s *Server) aggregateOpenAI(w http.ResponseWriter, resp *http.Response, req *oaiRequest, account *config.Account, apiKeyID string) {
	var textOut strings.Builder
	var reasoningOut strings.Builder
	tools := map[string]*toolAccum{}
	var toolOrder []string
	stopReason := "END_TURN"
	var usage, contextPct float64
	var sawMetering bool
	var tokens TokenMeter

	kiroFrameReader(resp.Body, func(et string, payload []byte) {
		tokens.Observe(payload)
		switch et {
		case "reasoningContentEvent":
			var o struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(payload, &o) == nil {
				reasoningOut.WriteString(o.Text)
			}
		case "assistantResponseEvent":
			textOut.WriteString(parseAssistantText(payload))
		case "toolUseEvent":
			tid, name, frag, _ := parseToolUse(payload)
			if tid == "" {
				return
			}
			t, ok := tools[tid]
			if !ok {
				t = &toolAccum{id: tid, name: name}
				tools[tid] = t
				toolOrder = append(toolOrder, tid)
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
			if n, ok := creditFromPayload(payload); ok {
				usage += n
				sawMetering = true
			}
		}
	})

	msg := map[string]any{"role": "assistant"}
	if textOut.Len() > 0 {
		msg["content"] = textOut.String()
	} else {
		msg["content"] = nil
	}
	if reasoningOut.Len() > 0 {
		msg["reasoning_content"] = reasoningOut.String()
	}
	if len(toolOrder) > 0 {
		var tcs []map[string]any
		for _, tid := range toolOrder {
			t := tools[tid]
			args := strings.TrimSpace(t.input.String())
			if args == "" {
				args = "{}"
			}
			tcs = append(tcs, map[string]any{
				"id": t.id, "type": "function",
				"function": map[string]any{"name": t.name, "arguments": args},
			})
		}
		msg["tool_calls"] = tcs
	}

	promptTok := estOpenAITokens(req)
	compTok := estOutputTokens(textOut.Len())
	out := map[string]any{
		"id": newChatID(), "object": "chat.completion", "created": time.Now().Unix(),
		"model": req.Model,
		"choices": []map[string]any{{
			"index": 0, "message": msg, "finish_reason": mapFinishReason(stopReason),
		}},
		"usage": map[string]any{
			"prompt_tokens": promptTok, "completion_tokens": compTok, "total_tokens": promptTok + compTok,
		},
	}
	writeJSON(w, 200, out)
	s.recordChatUsage(account, apiKeyID, usage, contextPct, sawMetering, tokens)
}

func estOpenAITokens(req *oaiRequest) int {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	if n < 4 {
		return 1
	}
	return n / 4
}
