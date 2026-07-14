package build

import (
	"encoding/json"
	"testing"
)

func TestNormalizeChatResponseFormatJSONObject(t *testing.T) {
	got, err := NormalizeChatResponseFormat(map[string]any{"type": "json_object"})
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "json_object" {
		t.Fatalf("got=%#v", got)
	}
}

func TestNormalizeChatResponseFormatJSONSchemaLiftsFields(t *testing.T) {
	// schema 内 type:object 不得覆盖 format type=json_schema
	raw := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "weather",
			"strict": true,
			"type":   "object", // 必须跳过
			"schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
			},
		},
	}
	got, err := NormalizeChatResponseFormat(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got["type"] != "json_schema" {
		t.Fatalf("type=%v want json_schema", got["type"])
	}
	if got["name"] != "weather" {
		t.Fatalf("name=%v", got["name"])
	}
	if _, hasNested := got["json_schema"]; hasNested {
		t.Fatalf("json_schema should be lifted, got=%#v", got)
	}
	// 内层 schema.type=object 仍在 schema 字段里
	schema, ok := got["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("schema=%#v", got["schema"])
	}
}

func TestNormalizeChatResponseFormatEmpty(t *testing.T) {
	got, err := NormalizeChatResponseFormat(nil)
	if err != nil || got != nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
	got, err = NormalizeChatResponseFormat("")
	if err != nil || got != nil {
		t.Fatalf("empty string got=%v err=%v", got, err)
	}
}

func TestBuildResponsesBodyInjectsTextFormat(t *testing.T) {
	body, err := BuildResponsesBodyOpts(ResponsesBodyOptions{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "out",
				"schema": map[string]any{"type": "object"},
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
	if _, ok := payload["response_format"]; ok {
		t.Fatal("response_format must not remain on payload")
	}
	text, ok := payload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text missing: %#v", payload)
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("text.format missing: %#v", text)
	}
	if format["type"] != "json_schema" || format["name"] != "out" {
		t.Fatalf("format=%#v", format)
	}
}

func TestBuildResponsesBodyOmitsTextWhenNoFormat(t *testing.T) {
	body, err := BuildResponsesBodyOpts(ResponsesBodyOptions{
		Model:    "grok-4",
		Messages: []ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["text"]; ok {
		t.Fatalf("text should be absent: %#v", payload)
	}
}
