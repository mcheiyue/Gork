package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

const PromptOverhead = 4

func EstimateTokens(value any) int {
	text := strings.TrimSpace(coerceTokenText(value))
	if text == "" {
		return 0
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		if tokens, ok := estimateJSONTokens(text); ok {
			return tokens
		}
	}
	return estimateTokenText(text)
}

func EstimatePromptTokens(value any, overhead int) int {
	base := EstimateTokens(value)
	if base <= 0 {
		return 0
	}
	if overhead < 0 {
		overhead = 0
	}
	return base + overhead
}

func EstimateToolCallTokens(toolCalls []any) int {
	normalized := make([]any, 0, len(toolCalls))
	for _, call := range toolCalls {
		normalized = append(normalized, normalizeToolCall(call))
	}
	return EstimateTokens(normalized)
}

func coerceTokenText(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return "b'" + string(typed) + "'"
	}
	data, err := marshalTokenJSON(value)
	if err == nil {
		return data
	}
	return fmt.Sprint(value)
}

func marshalTokenJSON(value any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func estimateJSONTokens(text string) (int, bool) {
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return 0, false
	}
	return estimateJSONValueTokens(value), true
}

func estimateJSONValueTokens(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		tokens := 2
		for key, item := range typed {
			tokens += estimateTokenText(key) + 1 + estimateJSONValueTokens(item)
		}
		return tokens
	case []any:
		tokens := 2
		for _, item := range typed {
			tokens += estimateJSONValueTokens(item)
		}
		return tokens
	case string:
		return estimateTokenText(typed)
	case nil:
		return 1
	default:
		return estimateTokenText(fmt.Sprint(typed))
	}
}

func estimateTokenText(text string) int {
	tokens := 0
	wordLen := 0
	cjkLen := 0
	flushWord := func() {
		if wordLen > 0 {
			tokens += (wordLen + 9) / 10
			wordLen = 0
		}
	}
	flushCJK := func() {
		if cjkLen > 0 {
			tokens += (cjkLen + 1) / 2
			cjkLen = 0
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flushWord()
			flushCJK()
		case isCJK(r):
			flushWord()
			cjkLen++
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			flushCJK()
			wordLen++
		default:
			flushWord()
			flushCJK()
			tokens++
		}
	}
	flushWord()
	flushCJK()
	return tokens
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func normalizeToolCall(call any) any {
	if _, ok := call.(map[string]any); ok {
		return call
	}
	name, hasName := reflectedField(call, "Name")
	arguments, hasArguments := reflectedField(call, "Arguments")
	if !hasName || !hasArguments || isZero(name) || isZero(arguments) {
		return call
	}
	callID, ok := reflectedField(call, "CallID")
	if !ok {
		callID, _ = reflectedField(call, "CallId")
	}
	if isZero(callID) {
		callID = ""
	}
	return map[string]any{
		"id":        callID,
		"name":      name,
		"arguments": arguments,
	}
}

func reflectedField(value any, name string) (any, bool) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, false
	}
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, false
	}
	field := rv.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return nil, false
	}
	return field.Interface(), true
}

func isZero(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	return rv.IsValid() && rv.IsZero()
}
