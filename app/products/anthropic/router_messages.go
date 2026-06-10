package anthropic

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/platform"
)

type AnthropicMessagesRequest struct {
	Model       string           `json:"model"`
	Messages    []map[string]any `json:"messages"`
	System      any              `json:"system"`
	MaxTokens   *int             `json:"max_tokens"`
	Stream      *bool            `json:"stream"`
	Temperature *float64         `json:"temperature"`
	TopP        *float64         `json:"top_p"`
	Tools       []map[string]any `json:"tools"`
	ToolChoice  any              `json:"tool_choice"`
	Thinking    any              `json:"thinking"`
}

func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAnthropicMessagesRequest(r)
	if err != nil {
		writeAnthropicError(w, err)
		return
	}
	options, err := buildAnthropicMessagesOptions(req)
	if err != nil {
		writeAnthropicError(w, err)
		return
	}
	result, err := anthropicRouterMessages(r.Context(), options)
	if err != nil {
		writeAnthropicError(w, err)
		return
	}
	writeAnthropicResult(w, result)
}

func decodeAnthropicMessagesRequest(r *http.Request) (AnthropicMessagesRequest, error) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var req AnthropicMessagesRequest
	if err := decoder.Decode(&req); err != nil {
		return AnthropicMessagesRequest{}, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	return req, nil
}

func buildAnthropicMessagesOptions(req AnthropicMessagesRequest) (MessagesOptions, error) {
	if err := validateAnthropicMessagesRequest(req); err != nil {
		return MessagesOptions{}, err
	}
	return MessagesOptions{
		Model:       req.Model,
		Messages:    req.Messages,
		System:      req.System,
		Stream:      anthropicStreamEnabled(req.Stream),
		EmitThink:   anthropicThinkingEnabled(req.Thinking),
		Temperature: anthropicFloatDefault(req.Temperature, 0.8),
		TopP:        anthropicFloatDefault(req.TopP, 0.95),
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
	}, nil
}

func validateAnthropicMessagesRequest(req AnthropicMessagesRequest) error {
	spec, ok := model.Get(req.Model)
	if !ok || !spec.Enabled {
		return platform.NewValidationError(
			fmt.Sprintf("Model %q does not exist or you do not have access to it.", req.Model),
			"model",
			"model_not_found",
		)
	}
	if len(req.Messages) == 0 {
		return platform.NewValidationError("messages cannot be empty", "messages", "")
	}
	return nil
}

func anthropicStreamEnabled(value *bool) bool {
	if value != nil {
		return *value
	}
	return anthropicRouterBoolConfig("features.stream", true)
}

func anthropicThinkingEnabled(value any) bool {
	if value != nil {
		if mapped, ok := value.(map[string]any); ok {
			return anthropicString(mapped["type"], "") != "disabled"
		}
	}
	return anthropicRouterBoolConfig("features.thinking", true)
}
