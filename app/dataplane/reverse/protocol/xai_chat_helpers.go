package protocol

import (
	"fmt"
	"regexp"
	"strings"
)

var streamNormalizeKeyRe = regexp.MustCompile(`[^0-9A-Za-z_\x{4e00}-\x{9fff}]+`)

func (a *StreamAdapter) appendReasoning(events *[]FrameEvent, line, rollout, tag string, stepID *int) {
	var text, key string
	if a.summaryMode {
		text = strings.TrimSpace(line)
		if text == "" {
			return
		}
		key = streamNormalizeKey(text)
	} else {
		text = line
		if text == "" {
			return
		}
		key = rollout + ":" + text
	}
	if _, ok := a.emittedReasoningKeys[key]; ok {
		return
	}
	a.emittedReasoningKeys[key] = struct{}{}
	formatted := text
	if !strings.HasSuffix(formatted, "\n") {
		formatted += "\n"
	}
	a.ThinkingBuf = append(a.ThinkingBuf, formatted)
	*events = append(*events, FrameEvent{Kind: "thinking", Content: formatted, RolloutID: rollout, MessageTag: tag, MessageStepID: stepID})
}

func (a *StreamAdapter) flushPendingReasoning(events *[]FrameEvent) {
	if a.summaryMode && a.reasoning != nil {
		for _, line := range a.reasoning.Finalize() {
			a.appendReasoning(events, line, "", "summary", nil)
		}
	}
}

func (a *StreamAdapter) summarizeToolUsageSummary(resp map[string]any, rollout string, stepID *int) []string {
	toolName, args := extractToolInfo(resp)
	if toolName == "" || a.reasoning == nil {
		return nil
	}
	return a.reasoning.OnToolUsage(toolName, args, ReasoningInputOptions{Rollout: rollout, StepID: stepID})
}

func (a *StreamAdapter) formatToolCard(resp map[string]any, rollout string) string {
	toolName, args := extractToolInfo(resp)
	if toolName == "" {
		return ""
	}
	format, ok := toolFormat[toolName]
	if !ok {
		format = struct {
			emoji string
			keys  []string
		}{emoji: "🔧"}
	}
	displayArg := ""
	for _, key := range format.keys {
		if value := strings.TrimSpace(stringFromAny(args[key])); value != "" {
			displayArg = value
			break
		}
	}
	prefix := ""
	if rollout != "" {
		prefix = "[" + rollout + "] "
	}
	if displayArg != "" {
		return fmt.Sprintf("%s%s %s: %s", prefix, format.emoji, toolName, displayArg)
	}
	return fmt.Sprintf("%s%s %s", prefix, format.emoji, toolName)
}

func extractToolInfo(resp map[string]any) (string, map[string]any) {
	card, ok := resp["toolUsageCard"].(map[string]any)
	if !ok {
		return "", map[string]any{}
	}
	for key, value := range card {
		if key == "toolUsageCardId" {
			continue
		}
		obj, ok := value.(map[string]any)
		if !ok {
			continue
		}
		rawArgs, _ := obj["args"].(map[string]any)
		if rawArgs == nil {
			rawArgs = map[string]any{}
		}
		return camelToSnake(key), rawArgs
	}
	return "", map[string]any{}
}

func camelToSnake(value string) string {
	return strings.ToLower(camelBoundaryRe.ReplaceAllString(value, `${1}_${2}`))
}

func streamNormalizeKey(text string) string {
	lowered := strings.ToLower(text)
	lowered = urlKeyRe.ReplaceAllString(lowered, "")
	return streamNormalizeKeyRe.ReplaceAllString(lowered, "")
}

func boolFromAny(value any) bool {
	v, _ := value.(bool)
	return v
}

func truthyAny(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case map[string]any:
		return len(v) > 0
	case []any:
		return len(v) > 0
	default:
		return true
	}
}

func numberAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func intPointerFromAny(value any) *int {
	if i, ok := numberAsInt(value); ok {
		return &i
	}
	return nil
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func anySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	default:
		return nil
	}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneAnyMaps(in []map[string]any) []map[string]any {
	if in == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		out = append(out, cloneAnyMap(item))
	}
	return out
}
