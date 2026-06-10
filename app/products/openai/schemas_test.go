package openai

import (
	"encoding/json"
	"testing"
)

func TestSchemasChatCompletionDefaultsAndWideMessageContent(t *testing.T) {
	raw := []byte(`{
		"model": "grok-4.20-fast",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hello"}]}
		]
	}`)
	var req ChatCompletionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal err=%v", err)
	}
	if req.Temperature == nil || *req.Temperature != 0.8 {
		t.Fatalf("temperature=%v want 0.8", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.95 {
		t.Fatalf("top_p=%v want 0.95", req.TopP)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls=%v want true", req.ParallelToolCalls)
	}
	content, ok := req.Messages[0].Content.([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content=%#v want list payload", req.Messages[0].Content)
	}
}

func TestSchemasImageDefaultsMatchPythonModels(t *testing.T) {
	var gen ImageGenerationRequest
	if err := json.Unmarshal([]byte(`{"model":"grok-imagine-image","prompt":"draw"}`), &gen); err != nil {
		t.Fatal(err)
	}
	if gen.N != 1 || gen.Size != "1024x1024" || gen.ResponseFormat != "url" {
		t.Fatalf("generation defaults=%#v", gen)
	}

	var edit ImageEditRequest
	if err := json.Unmarshal([]byte(`{"model":"grok-imagine-image-edit","prompt":"edit","image":"data:image/png;base64,AA=="}`), &edit); err != nil {
		t.Fatal(err)
	}
	if edit.N != 1 || edit.Size != "1024x1024" || edit.ResponseFormat != "url" {
		t.Fatalf("edit defaults=%#v", edit)
	}

	var imageConfig ImageConfig
	if err := json.Unmarshal([]byte(`{}`), &imageConfig); err != nil {
		t.Fatal(err)
	}
	if imageConfig.N != 1 || imageConfig.Size != "1024x1024" || imageConfig.ResponseFormat != "" {
		t.Fatalf("image config defaults=%#v", imageConfig)
	}
}

func TestSchemasResponsesIgnoresExtraFields(t *testing.T) {
	raw := []byte(`{
		"model": "grok-4.20-fast",
		"input": [{"role": "user", "content": "hello"}],
		"unknown_extra": "ignored",
		"parallel_tool_calls": true,
		"tools": [{"type": "function"}]
	}`)
	var req ResponsesCreateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal err=%v", err)
	}
	if req.Model != "grok-4.20-fast" {
		t.Fatalf("model=%q", req.Model)
	}
	if len(req.Tools) != 1 || req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("responses ignored fields mismatch: %#v", req)
	}
}
