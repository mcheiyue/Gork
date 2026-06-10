package openai

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

func defaultRole(role string) string {
	if role == "" {
		return "assistant"
	}
	return role
}

func optionalNonNegative(values []int) int {
	if len(values) == 0 {
		return 0
	}
	return maxInt(0, values[0])
}

func asParsedToolCall(call any) (protocol.ParsedToolCall, bool) {
	switch typed := call.(type) {
	case protocol.ParsedToolCall:
		return typed, true
	case *protocol.ParsedToolCall:
		if typed == nil {
			return protocol.ParsedToolCall{}, false
		}
		return *typed, true
	default:
		return protocol.ParsedToolCall{}, false
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func marshalCompactJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
