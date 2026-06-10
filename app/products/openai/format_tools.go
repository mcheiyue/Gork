package openai

import "github.com/jiujiu532/grok2api/app/platform"

func MakeToolCallChunk(params ToolCallChunkParams) map[string]any {
	var toolCallDelta map[string]any
	if params.IsFirst {
		toolCallDelta = map[string]any{
			"index": params.Index,
			"id":    params.CallID,
			"type":  "function",
			"function": map[string]any{
				"name":      params.Name,
				"arguments": params.Arguments,
			},
		}
	} else {
		toolCallDelta = map[string]any{
			"index": params.Index,
			"function": map[string]any{
				"arguments": params.Arguments,
			},
		}
	}

	return map[string]any{
		"id":      params.ResponseID,
		"object":  "chat.completion.chunk",
		"created": formatNowUnix(),
		"model":   params.Model,
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": []any{toolCallDelta},
			},
		}},
	}
}

func MakeToolCallDoneChunk(params ToolCallDoneChunkParams) map[string]any {
	chunk := map[string]any{
		"id":      params.ResponseID,
		"object":  "chat.completion.chunk",
		"created": formatNowUnix(),
		"model":   params.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "tool_calls",
		}},
	}
	if params.Usage != nil {
		chunk["usage"] = params.Usage
	}
	return chunk
}

func MakeToolCallResponse(params ToolCallResponseParams) map[string]any {
	responseID := params.ResponseID
	if responseID == "" {
		responseID = MakeResponseID()
	}

	toolCalls := make([]any, 0, len(params.ToolCalls))
	for _, call := range params.ToolCalls {
		parsed, ok := asParsedToolCall(call)
		if !ok {
			continue
		}
		toolCalls = append(toolCalls, map[string]any{
			"id":   parsed.CallID,
			"type": "function",
			"function": map[string]any{
				"name":      parsed.Name,
				"arguments": parsed.Arguments,
			},
		})
	}

	usage := params.Usage
	if usage == nil {
		completionTokens := platform.EstimateToolCallTokens(params.ToolCalls)
		promptTokens := platform.EstimatePromptTokens(params.PromptContent, platform.PromptOverhead)
		usage = BuildUsage(promptTokens, completionTokens)
	}

	return map[string]any{
		"id":      responseID,
		"object":  "chat.completion",
		"created": formatNowUnix(),
		"model":   params.Model,
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": toolCalls,
			},
			"finish_reason": "tool_calls",
		}},
		"usage": usage,
	}
}
