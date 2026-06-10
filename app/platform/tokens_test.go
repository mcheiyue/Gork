package platform

import "testing"

type fakeToolCall struct {
	Name      string
	Arguments string
	CallID    string
}

func TestEstimateTokensCoercesAndTrimsInputs(t *testing.T) {
	if got := EstimateTokens(nil); got != 0 {
		t.Fatalf("EstimateTokens(nil) = %d", got)
	}
	if got := EstimateTokens("   "); got != 0 {
		t.Fatalf("EstimateTokens(blank) = %d", got)
	}
	if got := EstimateTokens("hello world"); got != 2 {
		t.Fatalf("EstimateTokens(hello world) = %d", got)
	}
	if got := EstimateTokens("你好世界"); got != 2 {
		t.Fatalf("EstimateTokens(CJK) = %d", got)
	}
	if got := EstimateTokens(`{"a":1}`); got != 5 {
		t.Fatalf("EstimateTokens(compact JSON) = %d", got)
	}
	if got, want := EstimateTokens(map[string]any{"a": 1}), EstimateTokens(`{"a":1}`); got != want {
		t.Fatalf("JSON coercion tokens = %d, want %d", got, want)
	}
}

func TestCoerceTokenTextUsesCompactUnescapedJSON(t *testing.T) {
	value := map[string]any{"text": "<tag>&"}
	if got, want := coerceTokenText(value), `{"text":"<tag>&"}`; got != want {
		t.Fatalf("coerceTokenText(JSON) = %q, want %q", got, want)
	}
}

func TestEstimatePromptTokensAddsOverheadOnlyForNonEmptyInputs(t *testing.T) {
	if got := EstimatePromptTokens("", PromptOverhead); got != 0 {
		t.Fatalf("EstimatePromptTokens(empty) = %d", got)
	}
	base := EstimateTokens("hello world")
	if got := EstimatePromptTokens("hello world", PromptOverhead); got != base+PromptOverhead {
		t.Fatalf("EstimatePromptTokens(default overhead) = %d", got)
	}
	if got := EstimatePromptTokens("hello world", PromptOverhead); got != 6 {
		t.Fatalf("EstimatePromptTokens(Python fixture) = %d", got)
	}
	if got := EstimatePromptTokens("hello world", -10); got != base {
		t.Fatalf("EstimatePromptTokens(negative overhead) = %d", got)
	}
}

func TestEstimateToolCallTokensNormalizesStructCalls(t *testing.T) {
	normalized := []any{map[string]any{
		"id":        "call-1",
		"name":      "tool",
		"arguments": `{"x":1}`,
	}}
	got := EstimateToolCallTokens([]any{fakeToolCall{Name: "tool", Arguments: `{"x":1}`, CallID: "call-1"}})
	want := EstimateTokens(normalized)
	if got != want {
		t.Fatalf("EstimateToolCallTokens(struct) = %d, want %d", got, want)
	}
	if got != 21 {
		t.Fatalf("EstimateToolCallTokens(Python fixture) = %d", got)
	}
}
