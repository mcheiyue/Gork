package build

import (
	"encoding/json"
	"fmt"
	"strings"
)

// NormalizeChatTools 将 OpenAI chat.completions tools 收敛为 Build /responses function 工具。
// 支持：function（chat 嵌套与 responses 扁平）、namespace→扁平、最小 web_search。
// 不支持：服务端 tool_search、Codex 专用别名全套（后续可扩）。
func NormalizeChatTools(tools []map[string]any) ([]map[string]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, len(tools))
	seen := map[string]struct{}{}
	for i, tool := range tools {
		converted, err := normalizeOneTool(tool, fmt.Sprintf("tools[%d]", i))
		if err != nil {
			return nil, err
		}
		for _, item := range converted {
			key := toolDedupeKey(item)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	return out, nil
}

func normalizeOneTool(tool map[string]any, param string) ([]map[string]any, error) {
	if tool == nil {
		return nil, fmt.Errorf("%s 必须是对象", param)
	}
	typ := strings.ToLower(strings.TrimSpace(stringField(tool, "type")))
	switch typ {
	case "", "function":
		return []map[string]any{normalizeFunctionTool(tool)}, nil
	case "namespace":
		return normalizeNamespaceTool(tool, param)
	case "web_search", "web_search_preview":
		return []map[string]any{{"type": "web_search"}}, nil
	case "tool_search":
		exec := strings.ToLower(strings.TrimSpace(stringField(tool, "execution")))
		if exec == "" || exec == "server" {
			return nil, fmt.Errorf("Grok Build 不支持服务端 tool_search；请用 execution:\"client\"（暂未实现客户端 tool_search）")
		}
		return nil, fmt.Errorf("客户端 tool_search 尚未吸收，请先声明具体 function 工具")
	default:
		// 未知类型：若带 function 子对象仍当 function
		if _, ok := tool["function"].(map[string]any); ok {
			return []map[string]any{normalizeFunctionTool(tool)}, nil
		}
		return nil, fmt.Errorf("%s.type=%q 暂不支持", param, typ)
	}
}

func normalizeFunctionTool(tool map[string]any) map[string]any {
	// chat 形态: {type, function:{name,description,parameters}}
	if nested, ok := tool["function"].(map[string]any); ok {
		out := map[string]any{"type": "function"}
		if name := strings.TrimSpace(stringField(nested, "name")); name != "" {
			out["name"] = name
		}
		if desc := strings.TrimSpace(stringField(nested, "description")); desc != "" {
			out["description"] = desc
		}
		if params, ok := nested["parameters"]; ok {
			out["parameters"] = params
		} else if params, ok := nested["parameters_json"]; ok {
			out["parameters"] = params
		}
		return out
	}
	// responses 扁平: {type:function,name,description,parameters}
	out := map[string]any{"type": "function"}
	if name := strings.TrimSpace(stringField(tool, "name")); name != "" {
		out["name"] = name
	}
	if desc := strings.TrimSpace(stringField(tool, "description")); desc != "" {
		out["description"] = desc
	}
	if params, ok := tool["parameters"]; ok {
		out["parameters"] = params
	}
	return out
}

func normalizeNamespaceTool(tool map[string]any, param string) ([]map[string]any, error) {
	ns := strings.TrimSpace(stringField(tool, "name"))
	rawTools, ok := tool["tools"].([]any)
	if !ok {
		return nil, fmt.Errorf("%s.tools 必须是数组", param)
	}
	out := make([]map[string]any, 0, len(rawTools))
	for i, raw := range rawTools {
		child, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s.tools[%d] 必须是对象", param, i)
		}
		fn := normalizeFunctionTool(child)
		name := strings.TrimSpace(stringField(fn, "name"))
		if ns != "" && name != "" && !strings.Contains(name, "__") {
			fn["name"] = ns + "__" + name
		}
		out = append(out, fn)
	}
	return out, nil
}

func toolDedupeKey(tool map[string]any) string {
	return strings.ToLower(stringField(tool, "type") + "\x00" + stringField(tool, "name"))
}

// ExtractToolCallsFromResponses 从 /responses JSON 提取 OpenAI chat tool_calls。
func ExtractToolCallsFromResponses(raw []byte) []map[string]any {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	output, _ := payload["output"].([]any)
	var calls []map[string]any
	for _, item := range output {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(stringField(obj, "type")))
		if typ != "function_call" && typ != "custom_tool_call" {
			continue
		}
		name := strings.TrimSpace(stringField(obj, "name"))
		args := strings.TrimSpace(stringField(obj, "arguments"))
		if args == "" {
			if m, ok := obj["arguments"].(map[string]any); ok {
				b, _ := json.Marshal(m)
				args = string(b)
			}
		}
		if args == "" {
			args = "{}"
		}
		id := firstNonEmpty(stringField(obj, "call_id"), stringField(obj, "id"), "call_"+name)
		calls = append(calls, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      name,
				"arguments": args,
			},
		})
	}
	return calls
}
