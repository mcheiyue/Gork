package build

import (
	"encoding/json"
	"testing"
)

func TestBuildResponsesBodyMinimal(t *testing.T) {
	body, err := BuildResponsesBody("grok-4", []ChatMessage{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "grok-4" {
		t.Fatalf("model=%v", payload["model"])
	}
	if payload["instructions"] != "be brief" {
		t.Fatalf("instructions=%v", payload["instructions"])
	}
	if payload["input"] != "User: hi" {
		t.Fatalf("input=%v", payload["input"])
	}
	if payload["stream"] != false {
		t.Fatalf("stream=%v", payload["stream"])
	}
}

func TestChatCompletionFromResponsesJSON(t *testing.T) {
	raw := []byte(`{"output_text":"hello world","output":[]}`)
	got, err := ChatCompletionFromResponsesJSON("build/grok-4", "id-1", raw)
	if err != nil {
		t.Fatal(err)
	}
	choices := got["choices"].([]map[string]any)
	msg := choices[0]["message"].(map[string]any)
	if msg["content"] != "hello world" {
		t.Fatalf("content=%v", msg["content"])
	}
}

func TestExtractChatMessagesContentParts(t *testing.T) {
	msgs := ExtractChatMessages([]map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "part-a"},
			map[string]any{"type": "input_text", "text": "part-b"},
		}},
	})
	if len(msgs) != 1 || msgs[0].Content != "part-a\npart-b" {
		t.Fatalf("%#v", msgs)
	}
}

func TestBuildResponsesBodyRejectsEmpty(t *testing.T) {
	_, err := BuildResponsesBody("m", nil, false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildResponsesBodyOptsWithTools(t *testing.T) {
	body, err := BuildResponsesBodyOpts(ResponsesBodyOptions{
		Model: "grok-4",
		Messages: []ChatMessage{
			{Role: "user", Content: "weather?"},
		},
		Tools: []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":       "get_weather",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%#v", payload["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "get_weather" {
		t.Fatalf("%#v", tool)
	}
}

func TestChatCompletionFromResponsesJSONToolCalls(t *testing.T) {
	raw := []byte(`{"output":[{"type":"function_call","call_id":"c1","name":"get_weather","arguments":"{}"}]}`)
	got, err := ChatCompletionFromResponsesJSON("build/grok-4", "id-1", raw)
	if err != nil {
		t.Fatal(err)
	}
	choices := got["choices"].([]map[string]any)
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("%#v", choices[0])
	}
	msg := choices[0]["message"].(map[string]any)
	calls := msg["tool_calls"].([]map[string]any)
	if len(calls) != 1 {
		t.Fatalf("%#v", calls)
	}
}
