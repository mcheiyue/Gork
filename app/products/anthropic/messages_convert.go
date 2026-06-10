package anthropic

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func makeAnthropicMessageID() string {
	return "msg_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + randomAnthropicHex(4)
}

func makeAnthropicToolID() string {
	return "toolu_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + randomAnthropicHex(3)
}

func randomAnthropicHex(size int) string {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "000000"
	}
	return hex.EncodeToString(raw)
}

func anthropicContentToInternal(content any, role string) []map[string]any {
	if text, ok := content.(string); ok {
		return []map[string]any{{"role": role, "content": text}}
	}
	blocks := anthropicMapSlice(content)
	if len(blocks) == 0 {
		return nil
	}
	if resultBlocks := filterAnthropicBlocks(blocks, "tool_result"); len(resultBlocks) > 0 {
		return anthropicToolResultMessages(resultBlocks)
	}
	if len(filterAnthropicBlocks(blocks, "tool_use")) > 0 {
		return []map[string]any{anthropicAssistantToolMessage(blocks)}
	}
	normalized := normalizeAnthropicContentBlocks(blocks)
	if len(normalized) == 0 {
		return nil
	}
	return []map[string]any{{"role": role, "content": normalized}}
}

func anthropicToolResultMessages(blocks []map[string]any) []map[string]any {
	messages := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		messages = append(messages, map[string]any{
			"role":         "tool",
			"tool_call_id": anthropicString(block["tool_use_id"], ""),
			"content":      anthropicToolResultContent(block["content"]),
		})
	}
	return messages
}

func anthropicToolResultContent(content any) string {
	blocks := anthropicMapSlice(content)
	if len(blocks) == 0 {
		return anthropicString(content, "")
	}
	parts := []string{}
	for _, block := range blocks {
		if anthropicString(block["type"], "") == "text" {
			parts = append(parts, anthropicString(block["text"], ""))
		}
	}
	return strings.Join(parts, "\n")
}

func anthropicAssistantToolMessage(blocks []map[string]any) map[string]any {
	textParts := []string{}
	toolCalls := []map[string]any{}
	for _, block := range blocks {
		switch anthropicString(block["type"], "") {
		case "text":
			textParts = append(textParts, anthropicString(block["text"], ""))
		case "tool_use":
			toolCalls = append(toolCalls, anthropicToolCall(block))
		}
	}
	var content any
	if len(textParts) > 0 {
		content = strings.Join(textParts, " ")
	}
	return map[string]any{"role": "assistant", "content": content, "tool_calls": toolCalls}
}

func anthropicToolCall(block map[string]any) map[string]any {
	id := anthropicString(block["id"], "")
	if id == "" {
		id = makeAnthropicToolID()
	}
	return map[string]any{
		"id": id, "type": "function",
		"function": map[string]any{
			"name":      anthropicString(block["name"], ""),
			"arguments": compactAnthropicJSON(valueOrEmptyMap(block["input"])),
		},
	}
}

func normalizeAnthropicContentBlocks(blocks []map[string]any) []map[string]any {
	normalized := []map[string]any{}
	for _, block := range blocks {
		switch anthropicString(block["type"], "") {
		case "text":
			normalized = appendAnthropicTextBlock(normalized, block)
		case "image":
			normalized = appendAnthropicImageBlock(normalized, block)
		case "document":
			normalized = appendAnthropicDocumentBlock(normalized, block)
		}
	}
	return normalized
}

func appendAnthropicTextBlock(out []map[string]any, block map[string]any) []map[string]any {
	text := strings.TrimSpace(anthropicString(block["text"], ""))
	if text == "" {
		return out
	}
	return append(out, map[string]any{"type": "text", "text": text})
}

func appendAnthropicImageBlock(out []map[string]any, block map[string]any) []map[string]any {
	source := anthropicMap(block["source"])
	switch anthropicString(source["type"], "") {
	case "base64":
		media := anthropicString(source["media_type"], "image/jpeg")
		data := anthropicString(source["data"], "")
		return append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + media + ";base64," + data}})
	case "url":
		return append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": anthropicString(source["url"], "")}})
	}
	return out
}

func appendAnthropicDocumentBlock(out []map[string]any, block map[string]any) []map[string]any {
	source := anthropicMap(block["source"])
	if anthropicString(source["type"], "") != "base64" {
		return out
	}
	media := anthropicString(source["media_type"], "application/pdf")
	data := anthropicString(source["data"], "")
	return append(out, map[string]any{"type": "file", "file": map[string]any{"data": "data:" + media + ";base64," + data}})
}

func parseAnthropicMessages(messages []map[string]any, system any) []map[string]any {
	internal := []map[string]any{}
	if systemText := anthropicSystemText(system); strings.TrimSpace(systemText) != "" {
		internal = append(internal, map[string]any{"role": "system", "content": systemText})
	}
	for _, message := range messages {
		role := anthropicString(message["role"], "user")
		internal = append(internal, anthropicContentToInternal(message["content"], role)...)
	}
	return internal
}

func anthropicSystemText(system any) string {
	if system == nil {
		return ""
	}
	if text, ok := system.(string); ok {
		return text
	}
	blocks := anthropicMapSlice(system)
	if len(blocks) == 0 {
		return fmt.Sprint(system)
	}
	parts := []string{}
	for _, block := range blocks {
		if anthropicString(block["type"], "") == "text" {
			parts = append(parts, anthropicString(block["text"], ""))
		}
	}
	return strings.Join(parts, "\n")
}

func convertAnthropicTools(tools []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		result = append(result, map[string]any{"type": "function", "function": map[string]any{
			"name":        anthropicString(tool["name"], ""),
			"description": anthropicString(tool["description"], ""),
			"parameters":  tool["input_schema"],
		}})
	}
	return result
}

func convertAnthropicToolChoice(toolChoice any) any {
	if toolChoice == nil {
		return "auto"
	}
	if text, ok := toolChoice.(string); ok {
		return text
	}
	mapped := anthropicMap(toolChoice)
	switch anthropicString(mapped["type"], "auto") {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": anthropicString(mapped["name"], "")}}
	}
	return "auto"
}

func finishReasonToStopReason(finishReason string) string {
	switch finishReason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func buildAnthropicMessageResponse(id, modelName string, content []map[string]any, stopReason string, inputTokens, outputTokens int) map[string]any {
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": modelName,
		"content": content, "stop_reason": stopReason, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": outputTokens},
	}
}

func compactAnthropicJSON(value any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "{}"
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
