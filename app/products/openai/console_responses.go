package openai

import (
	"context"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

type consoleResponseOptions struct {
	Model       string
	Messages    []map[string]any
	Stream      *bool
	EmitThink   *bool
	Temperature float64
	TopP        float64
	ResponseID  string
	ReasoningID string
	MessageID   string
}

func ConsoleResponses(ctx context.Context, options consoleResponseOptions) (chatCompletionResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return chatCompletionResult{}, err
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}

	isStream := false
	if options.Stream != nil {
		isStream = *options.Stream
	}
	responseID := options.ResponseID
	if responseID == "" {
		responseID = MakeRespID("resp")
	}
	messageID := options.MessageID
	if messageID == "" {
		messageID = MakeRespID("msg")
	}
	maxRetries := chatSelectionMaxRetries()
	retryCodes := configuredRetryCodes(chatRetryConfig())
	excluded := []string{}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		account, ok, err := directory.ReserveChatAccount(ctx, spec, excluded)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if !ok {
			return chatCompletionResult{}, platform.NewRateLimitError("No available accounts for this model tier")
		}

		result, err := runConsoleResponseAttempt(ctx, options, account, responseID, messageID, isStream)
		finishChatAttempt(ctx, directory, account, err == nil, err)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if shouldRetryUpstream(err, retryCodes) && attempt < maxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return chatCompletionResult{}, err
	}
	if lastErr != nil {
		return chatCompletionResult{}, lastErr
	}
	return chatCompletionResult{}, platform.NewRateLimitError("No available accounts after retries")
}

func runConsoleResponseAttempt(ctx context.Context, options consoleResponseOptions, account chatAccount, responseID, messageID string, isStream bool) (chatCompletionResult, error) {
	upstreamStream := true
	payload := protocol.BuildConsolePayload(protocol.ConsolePayloadOptions{
		Messages:        options.Messages,
		Model:           options.Model,
		Temperature:     options.Temperature,
		TopP:            options.TopP,
		ReasoningEffort: consoleReasoningEffort(options.EmitThink),
		Stream:          &upstreamStream,
	})

	events, err := consoleStreamChat(ctx, account.Token, payload, chatTimeoutSeconds())
	if err != nil {
		return chatCompletionResult{}, err
	}

	adapter := protocol.NewConsoleStreamAdapter()
	frames := consoleResponseInitialFrames(responseID, options.Model, messageID, isStream)
	for _, event := range events {
		tokens, err := adapter.Feed(event.EventType, event.Data)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if isStream {
			for _, token := range tokens {
				frames = append(frames, consoleResponseTextDeltaFrame(messageID, token))
			}
		}
	}

	fullText := adapter.FullText()
	usage := consoleResponseUsage(adapter, options.Messages)
	output := []map[string]any{consoleResponseOutputItem(messageID, fullText)}
	response := MakeRespObject(RespObjectParams{
		ResponseID: responseID,
		Model:      options.Model,
		Status:     "completed",
		Output:     output,
		Usage:      usage,
	})
	if !isStream {
		return chatCompletionResult{Response: response}, nil
	}

	frames = append(frames,
		FormatSSE("response.output_text.done", map[string]any{
			"type":          "response.output_text.done",
			"item_id":       messageID,
			"output_index":  0,
			"content_index": 0,
			"text":          fullText,
		}),
		FormatSSE("response.content_part.done", map[string]any{
			"type":          "response.content_part.done",
			"item_id":       messageID,
			"output_index":  0,
			"content_index": 0,
			"part": map[string]any{
				"type":        "output_text",
				"text":        fullText,
				"annotations": []any{},
			},
		}),
		FormatSSE("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": 0,
			"item":         consoleResponseOutputItem(messageID, fullText),
		}),
		FormatSSE("response.completed", map[string]any{
			"type":     "response.completed",
			"response": response,
		}),
		"data: [DONE]\n\n",
	)
	return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
}

func consoleResponseInitialFrames(responseID, modelName, messageID string, isStream bool) []string {
	if !isStream {
		return nil
	}
	inProgress := func(eventType string) string {
		return FormatSSE(eventType, map[string]any{
			"type": eventType,
			"response": MakeRespObject(RespObjectParams{
				ResponseID: responseID,
				Model:      modelName,
				Status:     "in_progress",
				Output:     []map[string]any{},
			}),
		})
	}
	return []string{
		inProgress("response.created"),
		inProgress("response.in_progress"),
		FormatSSE("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item": map[string]any{
				"id":      messageID,
				"type":    "message",
				"role":    "assistant",
				"status":  "in_progress",
				"content": []any{},
			},
		}),
		FormatSSE("response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       messageID,
			"output_index":  0,
			"content_index": 0,
			"part": map[string]any{
				"type":        "output_text",
				"text":        "",
				"annotations": []any{},
			},
		}),
	}
}

func consoleResponseTextDeltaFrame(messageID, token string) string {
	return FormatSSE("response.output_text.delta", map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       messageID,
		"output_index":  0,
		"content_index": 0,
		"delta":         token,
	})
}

func consoleResponseOutputItem(messageID, text string) map[string]any {
	return map[string]any{
		"id":     messageID,
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{{
			"type": "output_text",
			"text": text,
		}},
	}
}

func consoleResponseUsage(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any) map[string]any {
	if adapter.Usage != nil {
		return BuildRespUsage(
			intFromAny(adapter.Usage["input_tokens"]),
			intFromAny(adapter.Usage["output_tokens"]),
		)
	}
	return BuildRespUsage(
		platform.EstimatePromptTokens(messages, platform.PromptOverhead),
		platform.EstimateTokens(adapter.FullText()),
	)
}
