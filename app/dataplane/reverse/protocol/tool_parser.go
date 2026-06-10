package protocol

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ParsedToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

type ParseResult struct {
	Calls         []ParsedToolCall
	SawToolSyntax bool
}

func NewParsedToolCall(name string, arguments any) ParsedToolCall {
	return ParsedToolCall{
		CallID:    newToolCallID(),
		Name:      name,
		Arguments: marshalToolArguments(arguments),
	}
}

func ParseToolCalls(text string, availableTools []string) ParseResult {
	result := ParseResult{}
	if strings.TrimSpace(text) == "" {
		return result
	}
	if !hasToolSyntax(text) {
		return result
	}
	result.SawToolSyntax = true

	calls := parseXMLToolCalls(text)
	if len(calls) == 0 {
		calls = parseJSONEnvelope(text)
	}
	if len(calls) == 0 {
		calls = parseJSONArray(text)
	}
	if len(calls) == 0 {
		calls = parseAltXML(text)
	}
	if len(calls) > 0 && len(availableTools) > 0 {
		calls = filterAvailableTools(calls, availableTools)
	}
	result.Calls = calls
	return result
}

var toolSyntaxRE = regexp.MustCompile(`(?i)<tool_calls|<tool_call|<function_call|<invoke\s|"tool_calls"\s*:|\btool_calls\b`)

func hasToolSyntax(text string) bool { return toolSyntaxRE.MatchString(text) }

var (
	xmlRootRE   = regexp.MustCompile(`(?is)<tool_calls\s*>(.*?)</tool_calls\s*>`)
	xmlCallRE   = regexp.MustCompile(`(?is)<tool_call\s*>(.*?)</tool_call\s*>`)
	xmlNameRE   = regexp.MustCompile(`(?is)<tool_name\s*>(.*?)</tool_name\s*>`)
	xmlParamsRE = regexp.MustCompile(`(?is)<parameters\s*>(.*?)</parameters\s*>`)
)

func parseXMLToolCalls(text string) []ParsedToolCall {
	rootMatch := xmlRootRE.FindStringSubmatch(text)
	if rootMatch == nil {
		return nil
	}
	var calls []ParsedToolCall
	for _, callMatch := range xmlCallRE.FindAllStringSubmatch(rootMatch[1], -1) {
		inner := callMatch[1]
		nameMatch := xmlNameRE.FindStringSubmatch(inner)
		if nameMatch == nil {
			continue
		}
		params := "{}"
		if paramsMatch := xmlParamsRE.FindStringSubmatch(inner); paramsMatch != nil {
			params = strings.TrimSpace(paramsMatch[1])
		}
		parsedArgs, ok := parseJSONTolerant(params)
		if !ok {
			continue
		}
		calls = append(calls, NewParsedToolCall(strings.TrimSpace(nameMatch[1]), parsedArgs))
	}
	return calls
}

func parseJSONEnvelope(text string) []ParsedToolCall {
	if !strings.Contains(text, `"tool_calls"`) {
		return nil
	}
	obj, ok := extractOutermostJSONObject(text)
	if !ok {
		return nil
	}
	rawCalls, ok := obj["tool_calls"].([]any)
	if !ok {
		return nil
	}
	return extractFromCallList(rawCalls)
}

func extractOutermostJSONObject(text string) (map[string]any, bool) {
	start := strings.Index(text, "{")
	if start == -1 {
		return nil, false
	}
	var obj map[string]any
	decoder := json.NewDecoder(strings.NewReader(text[start:]))
	if err := decoder.Decode(&obj); err == nil {
		return obj, true
	}
	end := strings.LastIndex(text, "}")
	if end <= start {
		return nil, false
	}
	value, ok := tryRepairJSON(text[start : end+1])
	if !ok {
		return nil, false
	}
	obj, ok = value.(map[string]any)
	return obj, ok
}

var jsonArrayRE = regexp.MustCompile(`(?s)\[[\s\S]+\]`)

func parseJSONArray(text string) []ParsedToolCall {
	match := jsonArrayRE.FindString(text)
	if match == "" {
		return nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(match), &arr); err != nil {
		return nil
	}
	return extractFromCallList(arr)
}

func extractFromCallList(items []any) []ParsedToolCall {
	var calls []ParsedToolCall
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(firstString(obj, "name", "tool_name"))
		if name == "" {
			continue
		}
		args := firstTruthy(obj, "input", "arguments", "parameters")
		if args == nil {
			args = map[string]any{}
		}
		calls = append(calls, NewParsedToolCall(name, args))
	}
	return calls
}

var (
	functionCallRE = regexp.MustCompile(`(?is)<function_call\s*>(.*?)</function_call\s*>`)
	invokeRE       = regexp.MustCompile(`(?is)<invoke\s+name=["']?(\w+)["']?\s*>(.*?)</invoke\s*>`)
	fcNameRE       = regexp.MustCompile(`(?is)<name\s*>(.*?)</name\s*>`)
	fcArgsRE       = regexp.MustCompile(`(?is)<arguments\s*>(.*?)</arguments\s*>`)
)

func parseAltXML(text string) []ParsedToolCall {
	var calls []ParsedToolCall
	for _, match := range functionCallRE.FindAllStringSubmatch(text, -1) {
		inner := match[1]
		nameMatch := fcNameRE.FindStringSubmatch(inner)
		if nameMatch == nil {
			continue
		}
		argsText := "{}"
		if argsMatch := fcArgsRE.FindStringSubmatch(inner); argsMatch != nil {
			argsText = strings.TrimSpace(argsMatch[1])
		}
		args, ok := parseJSONTolerant(argsText)
		if !ok {
			continue
		}
		calls = append(calls, NewParsedToolCall(strings.TrimSpace(nameMatch[1]), args))
	}
	for _, match := range invokeRE.FindAllStringSubmatch(text, -1) {
		args, ok := parseJSONTolerant(strings.TrimSpace(match[2]))
		if !ok {
			args = map[string]any{}
		}
		calls = append(calls, NewParsedToolCall(strings.TrimSpace(match[1]), args))
	}
	return calls
}

func parseJSONTolerant(text string) (any, bool) {
	if text == "" {
		return map[string]any{}, true
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err == nil {
		return value, true
	}
	return tryRepairJSON(text)
}

func tryRepairJSON(text string) (any, bool) {
	var buf bytes.Buffer
	escaped := false
	for _, r := range text {
		if r == '\n' && !escaped {
			buf.WriteString(`\n`)
		} else {
			buf.WriteRune(r)
		}
		escaped = r == '\\' && !escaped
		if r != '\\' {
			escaped = false
		}
	}
	var value any
	if err := json.Unmarshal(buf.Bytes(), &value); err != nil {
		return nil, false
	}
	return value, true
}

func marshalToolArguments(arguments any) string {
	if s, ok := arguments.(string); ok {
		return s
	}
	data, err := json.Marshal(arguments)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func filterAvailableTools(calls []ParsedToolCall, availableTools []string) []ParsedToolCall {
	allowed := make(map[string]bool, len(availableTools))
	for _, name := range availableTools {
		allowed[name] = true
	}
	var filtered []ParsedToolCall
	for _, call := range calls {
		if allowed[call.Name] {
			filtered = append(filtered, call)
		}
	}
	return filtered
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstTruthy(obj map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := obj[key]; ok && isTruthy(value) {
			return value
		}
	}
	return nil
}

func isTruthy(value any) bool {
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

func newToolCallID() string {
	random := make([]byte, 3)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("call_%d000000", time.Now().UnixMilli())
	}
	return fmt.Sprintf("call_%d%s", time.Now().UnixMilli(), hex.EncodeToString(random))
}
