package anthropic

import (
	"reflect"
	"testing"
)

func TestMessagesParseAnthropicContentBlocks(t *testing.T) {
	got := parseAnthropicMessages([]map[string]any{{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": " hello "},
			map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "abc"}},
			map[string]any{"type": "document", "source": map[string]any{"type": "base64", "media_type": "application/pdf", "data": "pdf"}},
		},
	}}, []any{
		map[string]any{"type": "text", "text": "system one"},
		map[string]any{"type": "text", "text": "system two"},
	})

	want := []map[string]any{
		{"role": "system", "content": "system one\nsystem two"},
		{"role": "user", "content": []map[string]any{
			{"type": "text", "text": "hello"},
			{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			{"type": "file", "file": map[string]any{"data": "data:application/pdf;base64,pdf"}},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("internal messages=%#v", got)
	}
}

func TestMessagesToolBlocksBecomeInternalMessages(t *testing.T) {
	assistant := anthropicContentToInternal([]any{
		map[string]any{"type": "text", "text": "I'll call it"},
		map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{"q": "go"}},
	}, "assistant")
	if len(assistant) != 1 {
		t.Fatalf("assistant=%#v", assistant)
	}
	call := assistant[0]["tool_calls"].([]map[string]any)[0]
	function := call["function"].(map[string]any)
	if call["id"] != "toolu_1" || function["name"] != "lookup" || function["arguments"] != `{"q":"go"}` {
		t.Fatalf("tool call=%#v", call)
	}

	tool := anthropicContentToInternal([]any{
		map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []any{
			map[string]any{"type": "text", "text": "one"},
			map[string]any{"type": "text", "text": "two"},
		}},
	}, "user")
	want := []map[string]any{{"role": "tool", "tool_call_id": "toolu_1", "content": "one\ntwo"}}
	if !reflect.DeepEqual(tool, want) {
		t.Fatalf("tool result=%#v", tool)
	}
}

func TestMessagesToolAndResponseHelpers(t *testing.T) {
	tools := convertAnthropicTools([]map[string]any{{
		"name": "lookup", "description": "find things",
		"input_schema": map[string]any{"type": "object"},
	}})
	function := tools[0]["function"].(map[string]any)
	if function["name"] != "lookup" || function["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("tools=%#v", tools)
	}
	if convertAnthropicToolChoice(nil) != "auto" {
		t.Fatalf("nil tool choice mismatch")
	}
	if convertAnthropicToolChoice(map[string]any{"type": "any"}) != "required" {
		t.Fatalf("any tool choice mismatch")
	}
	forced := convertAnthropicToolChoice(map[string]any{"type": "tool", "name": "lookup"}).(map[string]any)
	if forced["function"].(map[string]any)["name"] != "lookup" {
		t.Fatalf("forced tool choice=%#v", forced)
	}

	resp := buildAnthropicMessageResponse("msg_1", "grok", []map[string]any{{"type": "text", "text": "ok"}}, "end_turn", 3, 2)
	if resp["id"] != "msg_1" || resp["stop_sequence"] != nil {
		t.Fatalf("response=%#v", resp)
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != 3 || usage["output_tokens"] != 2 {
		t.Fatalf("usage=%#v", usage)
	}
	if finishReasonToStopReason("tool_calls") != "tool_use" || finishReasonToStopReason("length") != "max_tokens" {
		t.Fatalf("finish reason mapping mismatch")
	}
}
