package anthropic

import (
	"regexp"
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

var anthropicSourcesStripRE = regexp.MustCompile(`(?is)\n*\s*(?:Sources|Citations):\s*\n(?:[-*]\s+.*(?:\n|$))+`)

func extractAnthropicMessage(messages []map[string]any) (string, []string) {
	parts := []string{}
	files := []string{}
	for _, message := range messages {
		role := anthropicString(message["role"], "user")
		content := message["content"]
		toolCalls := anthropicMapSlice(message["tool_calls"])
		if role == "tool" {
			parts = appendToolResultPart(parts, message, content)
			continue
		}
		if role == "assistant" && len(toolCalls) > 0 {
			parts = appendAssistantToolCallPart(parts, content, toolCalls)
			continue
		}
		newParts, newFiles := extractAnthropicContentParts(role, content)
		parts = append(parts, newParts...)
		files = append(files, newFiles...)
	}
	return strings.Join(parts, "\n\n"), files
}

func appendToolResultPart(parts []string, message map[string]any, content any) []string {
	text := strings.TrimSpace(anthropicString(content, ""))
	if text == "" {
		return parts
	}
	label := "[tool result]"
	if id := anthropicString(message["tool_call_id"], ""); id != "" {
		label = "[tool result for " + id + "]"
	}
	return append(parts, label+":\n"+text)
}

func appendAssistantToolCallPart(parts []string, content any, toolCalls []map[string]any) []string {
	xml := protocol.ToolCallsToXML(toolCalls)
	text := strings.TrimSpace(anthropicString(content, ""))
	if text != "" {
		return append(parts, "[assistant]: "+text+"\n"+xml)
	}
	return append(parts, "[assistant]:\n"+xml)
}

func extractAnthropicContentParts(role string, content any) ([]string, []string) {
	if role == "assistant" {
		if text, ok := content.(string); ok {
			content = stripAnthropicGeneratedArtifacts(text, true)
		}
	}
	if text, ok := content.(string); ok {
		cleaned := stripAnthropicGeneratedArtifacts(strings.TrimSpace(text), false)
		if cleaned != "" {
			return []string{"[" + role + "]: " + cleaned}, nil
		}
		return nil, nil
	}
	return extractAnthropicBlockParts(role, anthropicMapSlice(content))
}

func extractAnthropicBlockParts(role string, blocks []map[string]any) ([]string, []string) {
	parts := []string{}
	files := []string{}
	for _, block := range blocks {
		switch anthropicString(block["type"], "") {
		case "text":
			text := stripAnthropicGeneratedArtifacts(strings.TrimSpace(anthropicString(block["text"], "")), role == "assistant")
			if text != "" {
				parts = append(parts, "["+role+"]: "+text)
			}
		case "image_url":
			files = appendNonEmpty(files, anthropicString(anthropicMap(block["image_url"])["url"], ""))
		case "input_audio", "file":
			files = appendNonEmpty(files, anthropicFileData(block))
		}
	}
	return parts, files
}

func anthropicFileData(block map[string]any) string {
	inner := anthropicMap(block[anthropicString(block["type"], "")])
	if data := anthropicString(inner["data"], ""); data != "" {
		return data
	}
	return anthropicString(inner["file_data"], "")
}

func appendNonEmpty(values []string, value string) []string {
	if value == "" {
		return values
	}
	return append(values, value)
}

func stripAnthropicGeneratedArtifacts(text string, stripSources bool) string {
	if text == "" {
		return text
	}
	if stripSources {
		text = anthropicSourcesStripRE.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}
