package openai

import (
	"encoding/json"
	"testing"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

func TestFormatIDsUsePythonCompatiblePrefixes(t *testing.T) {
	resetFormatForTest(t)

	if got, want := MakeResponseID(), "chatcmpl-1700000000123deadbeef"; got != want {
		t.Fatalf("MakeResponseID() = %q, want %q", got, want)
	}
	if got, want := MakeRespID("resp"), "resp_1700000000123deadbeef"; got != want {
		t.Fatalf("MakeRespID() = %q, want %q", got, want)
	}
}

func TestFormatUsageGolden(t *testing.T) {
	got := BuildUsage(-2, 3, 5)
	want := map[string]any{
		"prompt_tokens":     0,
		"completion_tokens": 3,
		"total_tokens":      3,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 0,
			"text_tokens":   0,
			"audio_tokens":  0,
			"image_tokens":  0,
		},
		"completion_tokens_details": map[string]any{
			"text_tokens":      -2,
			"audio_tokens":     0,
			"reasoning_tokens": 5,
		},
	}
	assertGoldenJSON(t, got, want)

	resp := BuildRespUsage(-3, 4, 2)
	assertGoldenJSON(t, resp, map[string]any{
		"input_tokens":  0,
		"output_tokens": 4,
		"total_tokens":  1,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": 2,
		},
	})
}

func TestFormatChatChunksGolden(t *testing.T) {
	resetFormatForTest(t)
	usage := BuildUsage(4, 5)
	annotations := []map[string]any{{"type": "url", "url": "https://example.test"}}

	chunk := MakeStreamChunk(StreamChunkParams{
		ResponseID:   "chatcmpl-test",
		Model:        "grok-2",
		Content:      "hello",
		Index:        1,
		IsFinal:      true,
		FinishReason: "stop",
		Usage:        usage,
		Annotations:  annotations,
	})
	assertGoldenJSON(t, chunk, map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "grok-2",
		"choices": []any{map[string]any{
			"index": 1,
			"delta": map[string]any{
				"role":        "assistant",
				"content":     "hello",
				"annotations": annotations,
			},
			"finish_reason": "stop",
		}},
		"usage": usage,
	})

	thinking := MakeThinkingChunk(ThinkingChunkParams{
		ResponseID: "chatcmpl-test",
		Model:      "grok-2",
		Content:    "reasoning",
		Index:      2,
	})
	assertGoldenJSON(t, thinking, map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "grok-2",
		"choices": []any{map[string]any{
			"index": 2,
			"delta": map[string]any{
				"role":              "assistant",
				"reasoning_content": "reasoning",
			},
		}},
	})
}

func TestFormatChatResponseGolden(t *testing.T) {
	resetFormatForTest(t)
	usage := BuildUsage(6, 7, 2)
	annotations := []map[string]any{{"type": "citation", "index": 0}}
	sources := []map[string]any{{"url": "https://example.test"}}

	got := MakeChatResponse(ChatResponseParams{
		ResponseID:       "chatcmpl-fixed",
		Model:            "grok-2",
		Content:          "answer",
		ReasoningContent: "thinking",
		Usage:            usage,
		SearchSources:    sources,
		Annotations:      annotations,
	})

	assertGoldenJSON(t, got, map[string]any{
		"id":      "chatcmpl-fixed",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "grok-2",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role":              "assistant",
				"content":           "answer",
				"reasoning_content": "thinking",
				"annotations":       annotations,
			},
			"finish_reason": "stop",
		}},
		"usage":          usage,
		"search_sources": sources,
	})
}

func TestFormatResponsesAPIGolden(t *testing.T) {
	resetFormatForTest(t)
	usage := BuildRespUsage(3, 4, 1)
	output := []map[string]any{{"type": "message", "content": "ok"}}

	obj := MakeRespObject(RespObjectParams{
		ResponseID: "resp-fixed",
		Model:      "grok-2",
		Status:     "completed",
		Output:     output,
		Usage:      usage,
	})
	assertGoldenJSON(t, obj, map[string]any{
		"id":         "resp-fixed",
		"object":     "response",
		"created_at": 1700000000,
		"status":     "completed",
		"model":      "grok-2",
		"output":     output,
		"usage":      usage,
	})

	if got, want := FormatSSE("response.completed", map[string]any{"delta": "ok", "sequence": 1}), "event: response.completed\ndata: {\"delta\":\"ok\",\"sequence\":1}\n\n"; got != want {
		t.Fatalf("FormatSSE() = %q, want %q", got, want)
	}
	if got, want := FormatSSE("response.output_text.delta", map[string]any{"text": "<>&"}), "event: response.output_text.delta\ndata: {\"text\":\"<>&\"}\n\n"; got != want {
		t.Fatalf("FormatSSE(html chars) = %q, want %q", got, want)
	}
}

func TestFormatToolCallsGolden(t *testing.T) {
	resetFormatForTest(t)
	usage := BuildUsage(1, 2)

	first := MakeToolCallChunk(ToolCallChunkParams{
		ResponseID: "chatcmpl-test",
		Model:      "grok-2",
		Index:      2,
		CallID:     "call_123",
		Name:       "search",
		Arguments:  "{\"q\":\"x\"}",
		IsFirst:    true,
	})
	assertGoldenJSON(t, first, map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "grok-2",
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{map[string]any{
					"index": 2,
					"id":    "call_123",
					"type":  "function",
					"function": map[string]any{
						"name":      "search",
						"arguments": "{\"q\":\"x\"}",
					},
				}},
			},
		}},
	})

	done := MakeToolCallDoneChunk(ToolCallDoneChunkParams{
		ResponseID: "chatcmpl-test",
		Model:      "grok-2",
		Usage:      usage,
	})
	assertGoldenJSON(t, done, map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "grok-2",
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": "tool_calls",
		}},
		"usage": usage,
	})

	response := MakeToolCallResponse(ToolCallResponseParams{
		ResponseID: "chatcmpl-fixed",
		Model:      "grok-2",
		ToolCalls: []any{
			protocol.ParsedToolCall{CallID: "call_123", Name: "search", Arguments: "{\"q\":\"x\"}"},
			"ignored",
		},
		Usage: usage,
	})
	assertGoldenJSON(t, response, map[string]any{
		"id":      "chatcmpl-fixed",
		"object":  "chat.completion",
		"created": 1700000000,
		"model":   "grok-2",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": nil,
				"tool_calls": []any{map[string]any{
					"id":   "call_123",
					"type": "function",
					"function": map[string]any{
						"name":      "search",
						"arguments": "{\"q\":\"x\"}",
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": usage,
	})
}

func resetFormatForTest(t *testing.T) {
	t.Helper()
	oldNowUnix := formatNowUnix
	oldNowMillis := formatNowMillis
	oldRandomHex := formatRandomHex
	formatNowUnix = func() int64 { return 1700000000 }
	formatNowMillis = func() int64 { return 1700000000123 }
	formatRandomHex = func() string { return "deadbeef" }
	t.Cleanup(func() {
		formatNowUnix = oldNowUnix
		formatNowMillis = oldNowMillis
		formatRandomHex = oldRandomHex
	})
}

func assertGoldenJSON(t *testing.T, got any, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}
