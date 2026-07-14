package build

import (
	"encoding/json"
	"testing"
)

func TestNormalizeChatToolsFunctionNestedAndFlat(t *testing.T) {
	tools, err := NormalizeChatTools([]map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "weather",
				"parameters":   map[string]any{"type": "object"},
			},
		},
		{
			"type":       "function",
			"name":       "lookup",
			"parameters": map[string]any{"type": "object"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("len=%d", len(tools))
	}
	if tools[0]["name"] != "get_weather" || tools[0]["type"] != "function" {
		t.Fatalf("%#v", tools[0])
	}
	if tools[1]["name"] != "lookup" {
		t.Fatalf("%#v", tools[1])
	}
}

func TestNormalizeChatToolsNamespace(t *testing.T) {
	tools, err := NormalizeChatTools([]map[string]any{
		{
			"type": "namespace",
			"name": "crm",
			"tools": []any{
				map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0]["name"] != "crm__lookup" {
		t.Fatalf("%#v", tools)
	}
}

func TestNormalizeChatToolsRejectsServerToolSearch(t *testing.T) {
	_, err := NormalizeChatTools([]map[string]any{
		{"type": "tool_search", "execution": "server"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExtractToolCallsFromResponses(t *testing.T) {
	raw := []byte(`{
		"output":[
			{"type":"function_call","call_id":"c1","name":"get_weather","arguments":"{\"city\":\"SF\"}"},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}
		]
	}`)
	calls := ExtractToolCallsFromResponses(raw)
	if len(calls) != 1 {
		t.Fatalf("%#v", calls)
	}
	fn := calls[0]["function"].(map[string]any)
	if calls[0]["id"] != "c1" || fn["name"] != "get_weather" {
		t.Fatalf("%#v", calls[0])
	}
	// 保证可 JSON 序列化
	if _, err := json.Marshal(calls); err != nil {
		t.Fatal(err)
	}
}
