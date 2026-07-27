package proxy

import (
	"encoding/json"
	"fmt"
	"kiro-cli-pool-proxy/auth"
	"net/http"
	"strings"
	"time"
)

// publishAccounts broadcasts the current sanitized account list to SSE
// subscribers so open cards re-render after any account mutation.
func (a *AdminHandler) publishAccounts() {
	if a.hub == nil {
		return
	}
	snap := a.cfg.UsageSnapshot()
	views := make([]accountView, 0, len(snap))
	for _, acc := range snap {
		views = append(views, toView(acc))
	}
	a.hub.Publish(Event{Type: "accounts", Data: views})
}

// apiEvents is the admin-panel SSE stream. Subscribers receive "log", "quota",
// and "accounts" events pushed in realtime, replacing the old polling loop.
func (a *AdminHandler) apiEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]string{"error": "streaming unsupported"})
		return
	}
	if a.hub == nil {
		writeJSON(w, 500, map[string]string{"error": "event hub unavailable"})
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := a.hub.Subscribe()
	defer unsubscribe()

	// Prime the connection so the client's onopen fires immediately.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// Heartbeat keeps intermediaries from closing an idle stream.
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			frame, err := encodeEvent(e)
			if err != nil {
				continue
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// apiSyncModels fetches ListAvailableModels through the first usable account and
// stores the advertised modelIds, refreshing the proxy's pass-through set.
func (a *AdminHandler) apiSyncModels(w http.ResponseWriter, r *http.Request) {
	snap := a.cfg.UsageSnapshot()
	var lastErr error
	var models []auth.KiroModel
	for _, acc := range snap {
		if !acc.Enabled {
			continue
		}
		prepared, err := a.prepareAccount(acc.ID)
		if err != nil {
			lastErr = err
			continue
		}
		ms, err := auth.ListAvailableModels(&prepared)
		if err != nil {
			lastErr = err
			continue
		}
		if len(ms) > 0 {
			models = ms
			break
		}
	}
	if len(models) == 0 {
		msg := "no models returned"
		if lastErr != nil {
			msg = lastErr.Error()
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": msg})
		return
	}

	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ModelId)
	}
	a.cfg.SetKiroModels(ids)
	a.cfg.Save()
	SetSyncedKiroModels(ids)
	writeJSON(w, 200, map[string]any{"ok": true, "models": models})
}

// apiTestModel probes a specific model through an account by issuing a minimal
// GenerateAssistantResponse turn — the Kiro-Go-style "test model" check.
func (a *AdminHandler) apiTestModel(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		Model string `json:"model"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	model := strings.TrimSpace(body.Model)
	if model == "" {
		model = "auto"
	}

	acc, err := a.prepareAccount(id)
	if err != nil {
		if err.Error() == "not found" {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	start := time.Now()
	reply, err := probeModel(&acc, model)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "model": model, "error": err.Error(), "latencyMs": latency})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "model": model, "reply": reply, "latencyMs": latency})
}
