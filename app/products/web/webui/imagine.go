package webui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

type webUIImagineOptions struct {
	AspectRatio string
	Count       int
	EnableNSFW  *bool
	EnablePro   bool
}

type webUIImagineStart struct {
	Prompt      string
	AspectRatio string
	Count       int
	Quality     string
	EnableNSFW  *bool
	EnablePro   bool
}

type webUIImagineRun struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
}

type webUIImagineSession struct {
	ws      *webUIWebSocket
	current *webUIImagineRun
}

func handleWebUIImagineWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWebUIJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"message": "Method not allowed"}})
		return
	}
	if !webUIImagineAllowed(r) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	ws, err := acceptWebUIWebSocket(w, r)
	if err != nil {
		return
	}
	defer ws.Close()
	session := &webUIImagineSession{ws: ws}
	defer session.stopRun()
	session.readLoop(context.Background())
}

func (s *webUIImagineSession) readLoop(ctx context.Context) {
	for {
		raw, err := s.ws.ReadText()
		if err != nil {
			return
		}
		s.handleMessage(ctx, raw)
	}
}

func (s *webUIImagineSession) handleMessage(ctx context.Context, raw string) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		_ = s.sendError("Invalid message format.", "invalid_payload")
		return
	}
	switch stringValue(payload["type"]) {
	case "start":
		s.handleStart(ctx, payload)
	case "stop":
		s.stopRun()
	default:
		_ = s.sendError("Unknown action.", "invalid_action")
	}
}

func (s *webUIImagineSession) handleStart(ctx context.Context, payload map[string]any) {
	start, ok := parseWebUIImagineStart(payload)
	if !ok {
		_ = s.sendError("Prompt cannot be empty.", "invalid_prompt")
		return
	}
	s.stopRun()
	runCtx, cancel := context.WithCancel(ctx)
	run := &webUIImagineRun{id: webUIImagineRunID(), cancel: cancel, done: make(chan struct{})}
	s.current = run
	go s.runImagine(runCtx, run, start)
}

func (s *webUIImagineSession) stopRun() {
	if s.current == nil {
		return
	}
	current := s.current
	s.current = nil
	current.cancel()
	<-current.done
}

func (s *webUIImagineSession) runImagine(ctx context.Context, run *webUIImagineRun, start webUIImagineStart) {
	defer close(run.done)
	_ = s.ws.WriteJSON(webUIImagineRunning(run.id, start))
	events, ok, err := webUIImagineEvents(ctx, start.Prompt, start.webOptions())
	if ctx.Err() != nil {
		_ = s.ws.WriteJSON(map[string]any{"type": "status", "status": "stopped", "run_id": run.id})
		return
	}
	if !ok {
		_ = s.sendError("No available accounts for this model tier", "rate_limit_exceeded")
		return
	}
	if err != nil {
		s.handleRunError(ctx, run.id, err)
		return
	}
	if s.forwardImagineEvents(ctx, run.id, events) {
		_ = s.ws.WriteJSON(map[string]any{"type": "status", "status": "completed", "run_id": run.id, "count": start.Count})
	}
}

func (s *webUIImagineSession) forwardImagineEvents(ctx context.Context, runID string, events []map[string]any) bool {
	for _, event := range events {
		if ctx.Err() != nil {
			_ = s.ws.WriteJSON(map[string]any{"type": "status", "status": "stopped", "run_id": runID})
			return false
		}
		if stringValue(event["type"]) == "_meta" {
			continue
		}
		out := copyImagineEvent(event, runID)
		_ = s.ws.WriteJSON(out)
		if stringValue(out["type"]) == "error" {
			return false
		}
	}
	return ctx.Err() == nil
}

func (s *webUIImagineSession) handleRunError(ctx context.Context, runID string, err error) {
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		_ = s.ws.WriteJSON(map[string]any{"type": "status", "status": "stopped", "run_id": runID})
		return
	}
	_ = s.sendError(err.Error(), "internal_error")
}

func (s *webUIImagineSession) sendError(message, code string) error {
	return s.ws.WriteJSON(map[string]any{"type": "error", "message": message, "code": code})
}
