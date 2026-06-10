package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const toolSystemHeader = `You have access to the following tools.

AVAILABLE TOOLS:
{tool_definitions}

TOOL CALL FORMAT — follow these rules exactly:
- When calling a tool, output ONLY the XML block below. No text before or after it.
- <parameters> must be a single-line valid JSON object (no line breaks inside).
- Place multiple tool calls inside ONE <tool_calls> element.
- Do NOT use markdown code fences around the XML.
- Do NOT output any inner monologue or explanation alongside the XML.

<tool_calls>
  <tool_call>
    <tool_name>TOOL_NAME</tool_name>
    <parameters>{{"key": "value"}}</parameters>
  </tool_call>
</tool_calls>

WRONG (never do this):
` + "```xml" + `
<tool_calls>...</tool_calls>
` + "```" + `
I'll call the search tool now. <tool_calls>...</tool_calls>

{tool_choice_instruction}
NOTE: Even if you believe you cannot fulfill the request, you must still follow the WHEN TO CALL rule above.`

const (
	choiceAuto     = "WHEN TO CALL: Call a tool when it is clearly needed. Otherwise respond in plain text."
	choiceNone     = "WHEN TO CALL: Do NOT call any tools. Respond in plain text only."
	choiceRequired = "WHEN TO CALL: You MUST output a <tool_calls> XML block. Do NOT write any plain-text reply. If you are uncertain, still call the most relevant tool with your best guess at the parameters."
	choiceForced   = "WHEN TO CALL: You MUST output a <tool_calls> XML block calling the tool named \"%s\". Do NOT write any plain-text reply under any circumstances."
)

func BuildToolSystemPrompt(tools []map[string]any, toolChoice any) string {
	prompt := strings.ReplaceAll(toolSystemHeader, "{tool_definitions}", formatToolDefinitions(tools))
	return strings.ReplaceAll(prompt, "{tool_choice_instruction}", buildChoiceInstruction(toolChoice))
}

func ExtractToolNames(tools []map[string]any) []string {
	var names []string
	for _, tool := range tools {
		name := strings.TrimSpace(stringFromMap(functionMap(tool), "name"))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func InjectIntoMessage(message, systemPrompt string) string {
	return "[system]: " + systemPrompt + "\n\n" + message
}

func ToolCallsToXML(toolCalls []map[string]any) string {
	lines := []string{"<tool_calls>"}
	for _, tc := range toolCalls {
		function := functionMap(tc)
		name := stringFromMap(function, "name")
		args := valueToString(valueOrDefault(function, "arguments", "{}"))
		if normalized, ok := normalizeJSONString(args); ok {
			args = normalized
		}
		lines = append(lines,
			"  <tool_call>",
			"    <tool_name>"+name+"</tool_name>",
			"    <parameters>"+args+"</parameters>",
			"  </tool_call>",
		)
	}
	lines = append(lines, "</tool_calls>")
	return strings.Join(lines, "\n")
}

func formatToolDefinitions(tools []map[string]any) string {
	parts := make([]string, 0, len(tools))
	for _, tool := range tools {
		function := functionMap(tool)
		name := strings.TrimSpace(stringFromMap(function, "name"))
		desc := strings.TrimSpace(stringFromMap(function, "description"))
		params, hasParams := function["parameters"]
		lines := []string{"Tool: " + name}
		if desc != "" {
			lines = append(lines, "Description: "+desc)
		}
		if hasParams && isPromptTruthy(params) {
			if encoded, ok := pythonStyleJSON(params); ok {
				lines = append(lines, "Parameters: "+encoded)
			} else {
				lines = append(lines, "Parameters: "+fmt.Sprint(params))
			}
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

func buildChoiceInstruction(toolChoice any) string {
	if toolChoice == nil || toolChoice == "auto" {
		return choiceAuto
	}
	if toolChoice == "none" {
		return choiceNone
	}
	if toolChoice == "required" {
		return choiceRequired
	}
	choice, ok := toolChoice.(map[string]any)
	if !ok {
		return choiceAuto
	}
	switch stringFromMap(choice, "type") {
	case "none":
		return choiceNone
	case "required":
		return choiceRequired
	case "function":
		forcedName := strings.TrimSpace(stringFromMap(functionMap(choice), "name"))
		if forcedName != "" {
			return fmt.Sprintf(choiceForced, forcedName)
		}
	}
	return choiceAuto
}

func functionMap(parent map[string]any) map[string]any {
	if parent == nil {
		return map[string]any{}
	}
	if value, ok := parent["function"].(map[string]any); ok {
		return value
	}
	return map[string]any{}
}

func stringFromMap(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func valueOrDefault(values map[string]any, key string, defaultValue any) any {
	if value, ok := values[key]; ok {
		return value
	}
	return defaultValue
}

func valueToString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func normalizeJSONString(text string) (string, bool) {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return "", false
	}
	encoded, ok := compactJSON(value)
	return encoded, ok
}

func compactJSON(value any) (string, bool) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", false
	}
	return strings.TrimSpace(buf.String()), true
}

func pythonStyleJSON(value any) (string, bool) {
	compact, ok := compactJSON(value)
	if !ok {
		return "", false
	}
	return addPythonJSONSpaces(compact), true
}

func addPythonJSONSpaces(text string) string {
	var out strings.Builder
	inString := false
	escaped := false
	for _, r := range text {
		out.WriteRune(r)
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if !inString && (r == ':' || r == ',') {
			out.WriteRune(' ')
		}
	}
	return out.String()
}

func isPromptTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case bool:
		return typed
	case int:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	case uint:
		return typed != 0
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case float32:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
