package build

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ChatStreamFramesFromResponsesSSE 将 Build /responses SSE（或整包 JSON）转为
// OpenAI chat.completion.chunk 的 data 帧列表（含末尾 data: [DONE]）。
func ChatStreamFramesFromResponsesSSE(model, responseID string, r io.Reader) ([]string, error) {
	if responseID == "" {
		responseID = "chatcmpl-build"
	}
	raw, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read build stream: %w", err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty build stream body")
	}
	// 非 SSE：整包 JSON 一次性输出
	if !strings.Contains(trimmed, "data:") && strings.HasPrefix(trimmed, "{") {
		text, err := extractOutputText(raw)
		if err != nil {
			return nil, err
		}
		return framesFromTextDeltas(model, responseID, []string{text}), nil
	}
	deltas, err := collectSSETextDeltas(trimmed)
	if err != nil {
		return nil, err
	}
	if len(deltas) == 0 {
		// 尝试从 SSE data 里拼最终 JSON
		if text, ok := extractTextFromSSEJSONBlobs(trimmed); ok {
			return framesFromTextDeltas(model, responseID, []string{text}), nil
		}
		return nil, fmt.Errorf("build stream 无可用文本 delta")
	}
	return framesFromTextDeltas(model, responseID, deltas), nil
}

func collectSSETextDeltas(payload string) ([]string, error) {
	var deltas []string
	scanner := bufio.NewScanner(strings.NewReader(payload))
	// 单行可能很长
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2<<20)
	var dataLines []string
	flush := func() {
		if len(dataLines) == 0 {
			return
		}
		data := strings.TrimSpace(strings.Join(dataLines, "\n"))
		dataLines = nil
		if data == "" || data == "[DONE]" {
			return
		}
		if pieces := textDeltasFromEventData(data); len(pieces) > 0 {
			deltas = append(deltas, pieces...)
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			continue
		}
		// 忽略 event:/id:/retry:
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return deltas, nil
}

func textDeltasFromEventData(data string) []string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
	}
	typ, _ := payload["type"].(string)
	switch typ {
	case "response.output_text.delta", "response.text.delta", "response.content_part.delta":
		if s := stringField(payload, "delta"); s != "" {
			return []string{s}
		}
		if s := stringField(payload, "text"); s != "" {
			return []string{s}
		}
	case "response.output_text.done", "response.completed", "response.done":
		// 结束事件不产生 delta
		return nil
	}
	// 无 type：尝试 output_text / choices
	if s := stringField(payload, "delta"); s != "" {
		return []string{s}
	}
	if s, err := extractOutputText([]byte(data)); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

func extractTextFromSSEJSONBlobs(payload string) (string, bool) {
	var parts []string
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if s, err := extractOutputText([]byte(data)); err == nil && s != "" {
			parts = append(parts, s)
		}
	}
	joined := strings.TrimSpace(strings.Join(parts, ""))
	return joined, joined != ""
}

func framesFromTextDeltas(model, responseID string, deltas []string) []string {
	now := time.Now().Unix()
	frames := make([]string, 0, len(deltas)+2)
	// 首帧带 role
	first := true
	for _, delta := range deltas {
		if delta == "" {
			continue
		}
		deltaMap := map[string]any{"content": delta}
		if first {
			deltaMap["role"] = "assistant"
			first = false
		}
		chunk := map[string]any{
			"id":      responseID,
			"object":  "chat.completion.chunk",
			"created": now,
			"model":   model,
			"choices": []any{map[string]any{
				"index": 0,
				"delta": deltaMap,
			}},
		}
		frames = append(frames, mustDataFrame(chunk))
	}
	// 结束帧
	final := map[string]any{
		"id":      responseID,
		"object":  "chat.completion.chunk",
		"created": now,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "stop",
		}},
	}
	frames = append(frames, mustDataFrame(final), "data: [DONE]\n\n")
	return frames
}

func mustDataFrame(payload any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return "data: {}\n\n"
	}
	return "data: " + string(data) + "\n\n"
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
