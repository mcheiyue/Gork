package anthropic

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func messagesFromStream(ctx context.Context, options MessagesOptions, plan messagesPlan, account messagesAccount) (MessagesResult, error) {
	lines, err := messagesStream(ctx, messagesStreamOptions{
		Token: account.Token, ModeID: account.ModeID, Message: plan.Message,
		Files: plan.Files, TimeoutSeconds: plan.TimeoutSeconds,
	})
	if err != nil {
		return MessagesResult{}, err
	}
	if plan.IsStream {
		return messagesStreamResult(ctx, options, plan, account.Token, lines)
	}
	return messagesNonStreamResult(ctx, options, plan, account.Token, lines)
}

func messagesNonStreamResult(ctx context.Context, options MessagesOptions, plan messagesPlan, token string, lines []string) (MessagesResult, error) {
	adapter, err := feedMessagesAdapter(lines)
	if err != nil {
		return MessagesResult{}, err
	}
	fullText := strings.Join(adapter.TextBuf, "")
	fullText = appendMessagesResolvedImages(ctx, token, fullText, adapter)
	if references := adapter.ReferencesSuffix(); references != "" {
		fullText += references
	}
	inputTokens := platform.EstimatePromptTokens(plan.Message, platform.PromptOverhead)
	fullThink := messagesThinkingText(options.EmitThink, adapter)
	outputTokens := platform.EstimateTokens(fullText) + platform.EstimateTokens(fullThink)
	if toolResponse := messagesToolResponse(plan, adapter, fullText, inputTokens); toolResponse != nil {
		return MessagesResult{Response: toolResponse}, nil
	}
	return MessagesResult{Response: messagesTextResponse(plan, adapter, fullText, inputTokens, outputTokens)}, nil
}

func feedMessagesAdapter(lines []string) (*protocol.StreamAdapter, error) {
	adapter := protocol.NewStreamAdapter(protocol.StreamAdapterOptions{ShowSearchSources: true})
	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		events, err := adapter.Feed(data)
		if err != nil {
			return nil, err
		}
		if messagesSawSoftStop(events) {
			break
		}
	}
	return adapter, nil
}

func messagesSawSoftStop(events []protocol.FrameEvent) bool {
	for _, event := range events {
		if event.Kind == "soft_stop" {
			return true
		}
	}
	return false
}

func messagesThinkingText(emitThink bool, adapter *protocol.StreamAdapter) string {
	if !emitThink {
		return ""
	}
	return strings.Join(adapter.ThinkingBuf, "")
}

func messagesToolResponse(plan messagesPlan, adapter *protocol.StreamAdapter, fullText string, inputTokens int) map[string]any {
	if len(plan.ToolNames) == 0 {
		return nil
	}
	result := protocol.ParseToolCalls(fullText, plan.ToolNames)
	if len(result.Calls) == 0 {
		return nil
	}
	content := messagesToolUseContent(result.Calls)
	resp := buildAnthropicMessageResponse(plan.MessageID, plan.Spec.ModelName, content, "tool_use", inputTokens, messagesToolCallTokens(result.Calls))
	addMessagesSearchSources(resp, adapter)
	return resp
}

func messagesTextResponse(plan messagesPlan, adapter *protocol.StreamAdapter, fullText string, inputTokens, outputTokens int) map[string]any {
	content := []map[string]any{{"type": "text", "text": fullText}}
	if annotations := adapter.AnnotationsList(); len(annotations) > 0 {
		content[0]["annotations"] = annotations
	}
	resp := buildAnthropicMessageResponse(plan.MessageID, plan.Spec.ModelName, content, "end_turn", inputTokens, outputTokens)
	addMessagesSearchSources(resp, adapter)
	return resp
}

func messagesToolUseContent(calls []protocol.ParsedToolCall) []map[string]any {
	content := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		input := map[string]any{}
		_ = json.Unmarshal([]byte(call.Arguments), &input)
		content = append(content, map[string]any{"type": "tool_use", "id": call.CallID, "name": call.Name, "input": input})
	}
	return content
}

func messagesToolCallTokens(calls []protocol.ParsedToolCall) int {
	values := make([]any, 0, len(calls))
	for _, call := range calls {
		values = append(values, call)
	}
	return platform.EstimateToolCallTokens(values)
}

func appendMessagesResolvedImages(ctx context.Context, token string, fullText string, adapter *protocol.StreamAdapter) string {
	for _, image := range adapter.ImageURLs {
		resolved, err := messagesImageResolver(ctx, token, image.URL, image.ImageID)
		if err == nil && resolved != "" {
			if fullText != "" {
				fullText += "\n\n"
			}
			fullText += resolved
		}
	}
	return fullText
}

func addMessagesSearchSources(resp map[string]any, adapter *protocol.StreamAdapter) {
	if sources := adapter.SearchSourcesList(); len(sources) > 0 {
		resp["search_sources"] = sources
	}
}
