package protocol

import (
	"strings"
	"testing"
)

func TestBuildToolSystemPromptMatchesPythonTemplateAndChoices(t *testing.T) {
	tools := []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "Search things",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}

	prompt := BuildToolSystemPrompt(tools, nil)
	for _, want := range []string{
		"You have access to the following tools.",
		"Tool: search",
		"Description: Search things",
		`Parameters: {"type": "object"}`,
		"WHEN TO CALL: Call a tool when it is clearly needed.",
		"<tool_calls>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}

	nonePrompt := BuildToolSystemPrompt(tools, "none")
	if !strings.Contains(nonePrompt, "Do NOT call any tools") {
		t.Fatalf("none prompt missing no-tool instruction:\n%s", nonePrompt)
	}

	requiredPrompt := BuildToolSystemPrompt(tools, "required")
	if !strings.Contains(requiredPrompt, "You MUST output a <tool_calls> XML block") {
		t.Fatalf("required prompt missing required instruction:\n%s", requiredPrompt)
	}

	forcedPrompt := BuildToolSystemPrompt(tools, map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "search"},
	})
	if !strings.Contains(forcedPrompt, `calling the tool named "search"`) {
		t.Fatalf("forced prompt missing tool name:\n%s", forcedPrompt)
	}
}

func TestBuildToolSystemPromptSkipsPythonFalsyParameters(t *testing.T) {
	tools := []map[string]any{
		{"function": map[string]any{"name": "flag", "parameters": false}},
		{"function": map[string]any{"name": "zero", "parameters": 0}},
	}

	prompt := BuildToolSystemPrompt(tools, nil)
	for _, unwanted := range []string{"Parameters: false", "Parameters: 0"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("prompt contains Python-falsy parameters %q:\n%s", unwanted, prompt)
		}
	}
}

func TestBuildToolSystemPromptMatchesPythonChoiceMapAndFallback(t *testing.T) {
	nonePrompt := BuildToolSystemPrompt(nil, map[string]any{"type": "none"})
	if !strings.Contains(nonePrompt, "Do NOT call any tools") {
		t.Fatalf("dict none prompt missing no-tool instruction:\n%s", nonePrompt)
	}

	requiredPrompt := BuildToolSystemPrompt(nil, map[string]any{"type": "required"})
	if !strings.Contains(requiredPrompt, "You MUST output a <tool_calls> XML block") {
		t.Fatalf("dict required prompt missing required instruction:\n%s", requiredPrompt)
	}

	for _, choice := range []any{
		"auto",
		map[string]any{"type": "unknown"},
		map[string]any{"type": "function", "function": map[string]any{"name": " "}},
	} {
		prompt := BuildToolSystemPrompt(nil, choice)
		if !strings.Contains(prompt, "Call a tool when it is clearly needed") {
			t.Fatalf("fallback auto prompt missing auto instruction for %#v:\n%s", choice, prompt)
		}
		if strings.Contains(prompt, "calling the tool named") {
			t.Fatalf("fallback auto prompt unexpectedly forced a tool for %#v:\n%s", choice, prompt)
		}
	}
}

func TestBuildToolSystemPromptKeepsUnicodeParametersLikePython(t *testing.T) {
	prompt := BuildToolSystemPrompt([]map[string]any{
		{
			"function": map[string]any{
				"name":        "translate",
				"description": "翻译文本",
				"parameters": map[string]any{
					"description": "中文",
					"enum":        []any{"咖啡"},
				},
			},
		},
	}, nil)

	for _, want := range []string{"Description: 翻译文本", "中文", "咖啡"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing unicode %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, `\u4e2d`) || strings.Contains(prompt, `\u5496`) {
		t.Fatalf("prompt escaped unicode like Python ensure_ascii=True:\n%s", prompt)
	}
}

func TestExtractToolNamesAndInjectIntoMessageMatchPython(t *testing.T) {
	tools := []map[string]any{
		{"function": map[string]any{"name": " search "}},
		{"function": map[string]any{"name": ""}},
		{"type": "function"},
		{"function": map[string]any{"name": "lookup"}},
	}

	names := ExtractToolNames(tools)
	if len(names) != 2 || names[0] != "search" || names[1] != "lookup" {
		t.Fatalf("names = %#v, want search and lookup", names)
	}

	got := InjectIntoMessage("hello", "system prompt")
	if got != "[system]: system prompt\n\nhello" {
		t.Fatalf("injected message = %q", got)
	}
}

func TestToolCallsToXMLMatchesPythonDefaultsEmptyAndUnicode(t *testing.T) {
	empty := ToolCallsToXML(nil)
	if empty != "<tool_calls>\n</tool_calls>" {
		t.Fatalf("empty XML = %q", empty)
	}

	xml := ToolCallsToXML([]map[string]any{
		{"function": map[string]any{"name": "missing_args"}},
		{"function": map[string]any{"name": "unicode", "arguments": `{"text":"中文"}`}},
	})

	for _, want := range []string{
		"    <tool_name>missing_args</tool_name>",
		"    <parameters>{}</parameters>",
		"    <tool_name>unicode</tool_name>",
		`    <parameters>{"text":"中文"}</parameters>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("xml missing %q:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, `\u4e2d`) {
		t.Fatalf("xml escaped unicode like Python ensure_ascii=True:\n%s", xml)
	}
}

func TestToolCallsToXMLMatchesPythonNormalization(t *testing.T) {
	xml := ToolCallsToXML([]map[string]any{
		{"function": map[string]any{"name": "search", "arguments": "{\n  \"q\": \"hi\"\n}"}},
		{"function": map[string]any{"name": "raw", "arguments": "not-json"}},
	})

	want := strings.Join([]string{
		"<tool_calls>",
		"  <tool_call>",
		"    <tool_name>search</tool_name>",
		`    <parameters>{"q":"hi"}</parameters>`,
		"  </tool_call>",
		"  <tool_call>",
		"    <tool_name>raw</tool_name>",
		"    <parameters>not-json</parameters>",
		"  </tool_call>",
		"</tool_calls>",
	}, "\n")
	if xml != want {
		t.Fatalf("xml =\n%s\nwant =\n%s", xml, want)
	}
}
