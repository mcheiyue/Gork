package openai

import (
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func buildNonStreamChatResponse(options chatResponseBuildOptions) (map[string]any, error) {
	fullText := options.State.Text
	for _, imageText := range options.State.ImageTexts {
		if imageText == "" {
			continue
		}
		if fullText != "" {
			fullText += "\n\n"
		}
		fullText += imageText
	}
	if options.State.References != "" {
		fullText += options.State.References
	}

	thinkingText := ""
	if options.EmitThink {
		thinkingText = options.State.Thinking
	}

	if len(options.ToolNames) > 0 {
		parseResult := protocol.ParseToolCalls(fullText, options.ToolNames)
		if len(parseResult.Calls) > 0 {
			toolCalls := make([]any, 0, len(parseResult.Calls))
			for _, call := range parseResult.Calls {
				toolCalls = append(toolCalls, call)
			}
			usage := BuildUsage(
				platform.EstimatePromptTokens(options.Message, platform.PromptOverhead),
				platform.EstimateToolCallTokens(toolCalls),
			)
			response := MakeToolCallResponse(ToolCallResponseParams{
				Model:         options.Model,
				ToolCalls:     toolCalls,
				PromptContent: options.Message,
				ResponseID:    options.ResponseID,
				Usage:         usage,
			})
			if len(options.State.SearchSources) > 0 {
				response["search_sources"] = options.State.SearchSources
			}
			return response, nil
		}
	}

	reasoningTokens := 0
	if thinkingText != "" {
		reasoningTokens = platform.EstimateTokens(thinkingText)
	}
	usage := BuildUsage(
		platform.EstimatePromptTokens(options.Message, platform.PromptOverhead),
		platform.EstimateTokens(fullText)+reasoningTokens,
		reasoningTokens,
	)
	annotations := toChatAnnotations(options.State.Annotations)
	response := MakeChatResponse(ChatResponseParams{
		Model:            options.Model,
		Content:          fullText,
		PromptContent:    options.Message,
		ResponseID:       options.ResponseID,
		ReasoningContent: thinkingText,
		SearchSources:    options.State.SearchSources,
		Annotations:      annotations,
		Usage:            usage,
	})
	return response, nil
}
