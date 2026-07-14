package build

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ChatMessage 是 OpenAI chat messages 的最小子集。
type ChatMessage struct {
	Role    string
	Content string
}

// ResponsesBodyOptions 构造 Build POST /responses 请求体。
type ResponsesBodyOptions struct {
	Model          string
	Messages       []ChatMessage
	Stream         bool
	Tools          []map[string]any
	ToolChoice     any
	PromptCacheKey string // 已解析的上游 prompt_cache_key；空则不注入
	ResponseFormat any    // OpenAI chat response_format；归一为 text.format
}

// BuildResponsesBody 将 chat messages 转为 Build POST /responses 请求体。
// system→instructions，其余拼成 input 文本；可选 tools/tool_choice。
func BuildResponsesBody(model string, messages []ChatMessage, stream bool) ([]byte, error) {
	return BuildResponsesBodyOpts(ResponsesBodyOptions{
		Model: model, Messages: messages, Stream: stream,
	})
}

// BuildResponsesBodyOpts 带 tools 的请求体构造。
func BuildResponsesBodyOpts(opts ResponsesBodyOptions) ([]byte, error) {
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		return nil, fmt.Errorf("build responses model 为空")
	}
	var systemParts []string
	var turns []string
	for _, msg := range opts.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		switch role {
		case "system", "developer":
			systemParts = append(systemParts, text)
		case "assistant":
			turns = append(turns, "Assistant: "+text)
		case "tool":
			turns = append(turns, "Tool: "+text)
		default:
			turns = append(turns, "User: "+text)
		}
	}
	input := strings.TrimSpace(strings.Join(turns, "\n"))
	if input == "" {
		return nil, fmt.Errorf("empty message after chat→responses conversion")
	}
	payload := map[string]any{
		"model":  model,
		"input":  input,
		"stream": opts.Stream,
	}
	if len(systemParts) > 0 {
		payload["instructions"] = strings.Join(systemParts, "\n\n")
	}
	if len(opts.Tools) > 0 {
		normalized, err := NormalizeChatTools(opts.Tools)
		if err != nil {
			return nil, err
		}
		if len(normalized) > 0 {
			payload["tools"] = normalized
		}
	}
	if opts.ToolChoice != nil {
		payload["tool_choice"] = opts.ToolChoice
	}
	if key := strings.TrimSpace(opts.PromptCacheKey); key != "" {
		payload["prompt_cache_key"] = key
	}
	if format, err := NormalizeChatResponseFormat(opts.ResponseFormat); err != nil {
		return nil, err
	} else if format != nil {
		payload["text"] = map[string]any{"format": format}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal build responses body: %w", err)
	}
	return data, nil
}

// ChatCompletionFromResponsesJSON 将 Build /responses JSON 转为 OpenAI chat.completion。
// 若含 function_call，映射为 message.tool_calls 且 finish_reason=tool_calls。
func ChatCompletionFromResponsesJSON(model, responseID string, raw []byte) (map[string]any, error) {
	if responseID == "" {
		responseID = "chatcmpl-build"
	}
	now := time.Now().Unix()
	toolCalls := ExtractToolCallsFromResponses(raw)
	text, textErr := extractOutputText(raw)
	if len(toolCalls) > 0 {
		msg := map[string]any{
			"role":       "assistant",
			"content":    nil,
			"tool_calls": toolCalls,
		}
		if strings.TrimSpace(text) != "" {
			msg["content"] = text
		}
		return map[string]any{
			"id":      responseID,
			"object":  "chat.completion",
			"created": now,
			"model":   model,
			"choices": []map[string]any{{
				"index": 0, "message": msg, "finish_reason": "tool_calls",
			}},
			"usage": map[string]any{
				"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0,
			},
		}, nil
	}
	if textErr != nil {
		return nil, textErr
	}
	return map[string]any{
		"id":      responseID,
		"object":  "chat.completion",
		"created": now,
		"model":   model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role": "assistant", "content": text,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0,
		},
	}, nil
}

// ExtractChatMessages 从 []map[string]any 抽出 role/content 文本。
func ExtractChatMessages(messages []map[string]any) []ChatMessage {
	out := make([]ChatMessage, 0, len(messages))
	for _, item := range messages {
		role, _ := item["role"].(string)
		content := flattenContent(item["content"])
		if strings.TrimSpace(role) == "" && strings.TrimSpace(content) == "" {
			continue
		}
		out = append(out, ChatMessage{Role: role, Content: content})
	}
	return out
}

func flattenContent(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []any:
		var parts []string
		for _, part := range typed {
			switch p := part.(type) {
			case string:
				if s := strings.TrimSpace(p); s != "" {
					parts = append(parts, s)
				}
			case map[string]any:
				if t, _ := p["type"].(string); t == "text" || t == "input_text" || t == "output_text" {
					if s, _ := p["text"].(string); strings.TrimSpace(s) != "" {
						parts = append(parts, s)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func extractOutputText(raw []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("parse build responses json: %w", err)
	}
	// 优先 output_text
	if s, ok := payload["output_text"].(string); ok && strings.TrimSpace(s) != "" {
		return s, nil
	}
	// OpenAI Responses: output[].content[].text
	if output, ok := payload["output"].([]any); ok {
		var parts []string
		for _, item := range output {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			content, _ := obj["content"].([]any)
			for _, c := range content {
				part, ok := c.(map[string]any)
				if !ok {
					continue
				}
				typ, _ := part["type"].(string)
				if typ == "output_text" || typ == "text" {
					if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
						parts = append(parts, text)
					}
				}
			}
			// 部分响应 role=assistant 的 message 形态
			if role, _ := obj["role"].(string); role == "assistant" {
				if text, _ := obj["content"].(string); strings.TrimSpace(text) != "" {
					parts = append(parts, text)
				}
			}
		}
		if joined := strings.TrimSpace(strings.Join(parts, "")); joined != "" {
			return joined, nil
		}
	}
	// choices[0].message.content 兼容
	if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			if msg, ok := c0["message"].(map[string]any); ok {
				if text, _ := msg["content"].(string); strings.TrimSpace(text) != "" {
					return text, nil
				}
			}
			if text, _ := c0["text"].(string); strings.TrimSpace(text) != "" {
				return text, nil
			}
		}
	}
	return "", fmt.Errorf("build responses 无可用文本输出")
}
