package openai

import (
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func stripGeneratedArtifacts(text string, stripSources bool) string {
	if text == "" {
		return text
	}
	if stripSources {
		text = sourcesStripRE.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

func extractMessage(messages []map[string]any) (string, []string) {
	parts := []string{}
	files := []string{}

	for _, message := range messages {
		role := stringValue(message["role"], "user")
		content := valueOrDefaultAny(message["content"], "")
		toolCalls := toToolCalls(message["tool_calls"])

		if role == "tool" {
			toolCallID := stringValue(message["tool_call_id"], "")
			label := "[tool result]"
			if toolCallID != "" {
				label = "[tool result for " + toolCallID + "]"
			}
			text, ok := content.(string)
			if ok {
				text = strings.TrimSpace(text)
			}
			if text != "" {
				parts = append(parts, label+":\n"+text)
			}
			continue
		}

		if role == "assistant" && len(toolCalls) > 0 {
			xml := protocol.ToolCallsToXML(toolCalls)
			text, ok := content.(string)
			if ok {
				text = strings.TrimSpace(text)
			}
			if text != "" {
				parts = append(parts, "[assistant]: "+text+"\n"+xml)
			} else {
				parts = append(parts, "[assistant]:\n"+xml)
			}
			continue
		}

		if role == "assistant" {
			if text, ok := content.(string); ok {
				content = stripGeneratedArtifacts(text, true)
			}
		}

		switch typed := content.(type) {
		case string:
			cleaned := stripGeneratedArtifacts(strings.TrimSpace(typed), false)
			if cleaned != "" {
				parts = append(parts, "["+role+"]: "+cleaned)
			}
		case []any:
			for _, block := range typed {
				blockMap, ok := block.(map[string]any)
				if !ok {
					continue
				}
				blockType := stringValue(blockMap["type"], "")
				switch blockType {
				case "text":
					text := stripGeneratedArtifacts(strings.TrimSpace(stringValue(blockMap["text"], "")), role == "assistant")
					if text != "" {
						parts = append(parts, "["+role+"]: "+text)
					}
				case "image_url":
					imageURL, _ := blockMap["image_url"].(map[string]any)
					if file := stringValue(imageURL["url"], ""); file != "" {
						files = append(files, file)
					}
				case "input_audio", "file":
					inner, _ := blockMap[blockType].(map[string]any)
					data := stringValue(inner["data"], "")
					if data == "" {
						data = stringValue(inner["file_data"], "")
					}
					if data != "" {
						files = append(files, data)
					}
				}
			}
		}
	}

	return strings.Join(parts, "\n\n"), files
}

func isDigits(text string) bool {
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return text != ""
}

func stringValue(value any, defaultValue string) string {
	if value == nil {
		return defaultValue
	}
	if text, ok := value.(string); ok {
		return text
	}
	return defaultValue
}

func valueOrDefaultAny(value any, defaultValue any) any {
	if value == nil {
		return defaultValue
	}
	return value
}

func toToolCalls(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				result = append(result, mapped)
			}
		}
		return result
	default:
		return nil
	}
}

func errorsAs(err error, target any) bool {
	type causer interface{ Unwrap() error }
	for err != nil {
		switch typed := target.(type) {
		case **platform.UpstreamError:
			if upstream, ok := err.(*platform.UpstreamError); ok {
				*typed = upstream
				return true
			}
		}
		if wrapped, ok := err.(causer); ok {
			err = wrapped.Unwrap()
			continue
		}
		return false
	}
	return false
}

func upstreamStatus(err error) int {
	var upstream *platform.UpstreamError
	if errorsAs(err, &upstream) && upstream != nil {
		return upstream.Status
	}
	return 0
}
