package openai

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jiujiu532/grok2api/app/platform"
)

type StreamChunkParams struct {
	ResponseID   string
	Model        string
	Content      string
	Index        int
	Role         string
	IsFinal      bool
	FinishReason string
	Usage        map[string]any
	Annotations  []map[string]any
}

type ThinkingChunkParams struct {
	ResponseID string
	Model      string
	Content    string
	Index      int
	Role       string
}

type ChatResponseParams struct {
	Model            string
	Content          string
	PromptContent    any
	ResponseID       string
	Usage            map[string]any
	ReasoningContent string
	SearchSources    []map[string]any
	Annotations      []map[string]any
}

type RespObjectParams struct {
	ResponseID string
	Model      string
	Status     string
	Output     []map[string]any
	Usage      map[string]any
}

type ToolCallChunkParams struct {
	ResponseID string
	Model      string
	Index      int
	CallID     string
	Name       string
	Arguments  string
	IsFirst    bool
}

type ToolCallDoneChunkParams struct {
	ResponseID string
	Model      string
	Usage      map[string]any
}

type ToolCallResponseParams struct {
	Model         string
	ToolCalls     []any
	PromptContent any
	ResponseID    string
	Usage         map[string]any
}

var (
	formatNowUnix = func() int64 {
		return time.Now().Unix()
	}
	formatNowMillis = func() int64 {
		return time.Now().UnixMilli()
	}
	formatRandomHex = func() string {
		var data [4]byte
		if _, err := rand.Read(data[:]); err != nil {
			return "00000000"
		}
		return hex.EncodeToString(data[:])
	}
)

func MakeResponseID() string {
	return fmt.Sprintf("chatcmpl-%d%s", formatNowMillis(), formatRandomHex())
}

func BuildUsage(promptTokens, completionTokens int, reasoningTokens ...int) map[string]any {
	pt := maxInt(0, promptTokens)
	ct := maxInt(0, completionTokens)
	rt := optionalNonNegative(reasoningTokens)

	return map[string]any{
		"prompt_tokens":     pt,
		"completion_tokens": ct,
		"total_tokens":      pt + ct,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 0,
			"text_tokens":   pt,
			"audio_tokens":  0,
			"image_tokens":  0,
		},
		"completion_tokens_details": map[string]any{
			"text_tokens":      ct - rt,
			"audio_tokens":     0,
			"reasoning_tokens": rt,
		},
	}
}

func MakeStreamChunk(params StreamChunkParams) map[string]any {
	role := defaultRole(params.Role)
	choice := map[string]any{
		"index": params.Index,
		"delta": map[string]any{
			"role":    role,
			"content": params.Content,
		},
	}
	if params.IsFinal {
		finishReason := params.FinishReason
		if finishReason == "" {
			finishReason = "stop"
		}
		choice["finish_reason"] = finishReason
		if len(params.Annotations) > 0 {
			choice["delta"].(map[string]any)["annotations"] = params.Annotations
		}
	}

	chunk := map[string]any{
		"id":      params.ResponseID,
		"object":  "chat.completion.chunk",
		"created": formatNowUnix(),
		"model":   params.Model,
		"choices": []any{choice},
	}
	if params.Usage != nil {
		chunk["usage"] = params.Usage
	}
	return chunk
}

func MakeThinkingChunk(params ThinkingChunkParams) map[string]any {
	return map[string]any{
		"id":      params.ResponseID,
		"object":  "chat.completion.chunk",
		"created": formatNowUnix(),
		"model":   params.Model,
		"choices": []any{map[string]any{
			"index": params.Index,
			"delta": map[string]any{
				"role":              defaultRole(params.Role),
				"reasoning_content": params.Content,
			},
		}},
	}
}

func MakeChatResponse(params ChatResponseParams) map[string]any {
	responseID := params.ResponseID
	if responseID == "" {
		responseID = MakeResponseID()
	}

	promptTokens := platform.EstimatePromptTokens(params.PromptContent, platform.PromptOverhead)
	completionTokens := platform.EstimateTokens(params.Content)
	reasoningTokens := 0
	if params.ReasoningContent != "" {
		reasoningTokens = platform.EstimateTokens(params.ReasoningContent)
		completionTokens += reasoningTokens
	}

	message := map[string]any{
		"role":    "assistant",
		"content": params.Content,
	}
	if params.ReasoningContent != "" {
		message["reasoning_content"] = params.ReasoningContent
	}
	if len(params.Annotations) > 0 {
		message["annotations"] = params.Annotations
	}

	usage := params.Usage
	if usage == nil {
		usage = BuildUsage(promptTokens, completionTokens, reasoningTokens)
	}

	response := map[string]any{
		"id":      responseID,
		"object":  "chat.completion",
		"created": formatNowUnix(),
		"model":   params.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": "stop",
		}},
		"usage": usage,
	}
	if len(params.SearchSources) > 0 {
		response["search_sources"] = params.SearchSources
	}
	return response
}

func MakeRespID(prefix string) string {
	return fmt.Sprintf("%s_%d%s", prefix, formatNowMillis(), formatRandomHex())
}
