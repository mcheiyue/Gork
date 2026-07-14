package openai

import (
	"context"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
)

type consoleResponseOptions struct {
	Model       string
	Messages    []map[string]any
	Stream      *bool
	EmitThink   *bool
	Temperature float64
	TopP        float64
	Tools       []map[string]any
	ToolChoice  any
	ResponseID  string
	ReasoningID string
	MessageID   string
}

func ConsoleResponses(ctx context.Context, options consoleResponseOptions) (chatCompletionResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return chatCompletionResult{}, err
	}

	cooldownKey := consoleModelCooldownKey(options.Model)
	if rem := consoleCircuitBreaker.remainingCooldown(cooldownKey); rem > 0 {
		return chatCompletionResult{}, consoleRateLimitCooldownError(cooldownKey, rem, nil)
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
	rpmRetries := 0
	rpsRetries := 0
	unknown429Retries := 0

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

		if isConsoleRateLimitError(err) {
			errBody := extract429Body(err)
			info := parseConsole429Info(errBody)
			cooldown := consoleCircuitBreaker.tripModel(cooldownKey, info)

			var delay time.Duration
			allowRetry := false
			switch {
			case info.IsPerMinuteHit:
				if attempt < maxRetries && rpmRetries < 1 {
					rpmRetries++
					delay = time.Second
					allowRetry = true
				}
			case info.IsPerSecondHit:
				if attempt < maxRetries && rpsRetries < 2 {
					rpsRetries++
					delay = 2 * time.Second
					allowRetry = true
				}
			default:
				if attempt < maxRetries && unknown429Retries < 1 {
					unknown429Retries++
					delay = 2 * time.Second
					allowRetry = true
				}
			}
			if allowRetry {
				select {
				case <-time.After(delay):
					excluded = append(excluded, account.Token)
					continue
				case <-ctx.Done():
					return chatCompletionResult{}, ctx.Err()
				}
			}
			return chatCompletionResult{}, consoleRateLimitCooldownError(cooldownKey, cooldown, &info)
		}

		if shouldRetryUpstream(err, retryCodes) && attempt < maxRetries {
			excluded = append(excluded, account.Token)
			if waitErr := waitBeforeChatRetry(ctx, attempt); waitErr != nil {
				return chatCompletionResult{}, waitErr
			}
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
	clientToolNames := protocol.ClientConsoleToolNames(options.Tools)
	payload := protocol.BuildConsolePayload(protocol.ConsolePayloadOptions{
		Messages:          options.Messages,
		Model:             options.Model,
		Temperature:       options.Temperature,
		TopP:              options.TopP,
		ReasoningEffort:   consoleReasoningEffort(options.EmitThink),
		Stream:            &upstreamStream,
		CustomInstruction: chatCustomInstruction(),
		Tools:             options.Tools,
		ToolChoice:        options.ToolChoice,
	})

	events, err := consoleStreamChat(ctx, account.Token, payload, chatTimeoutSeconds())
	if err != nil {
		return chatCompletionResult{}, err
	}

	adapter := protocol.NewConsoleStreamAdapterWithTools(clientToolNames)
	bufferText := len(clientToolNames) > 0
	frames := consoleResponseLifecycleFrames(responseID, options.Model, isStream)
	if isStream && !bufferText {
		frames = append(frames, consoleResponseMessageStartFrames(messageID, 0)...)
	}
	for _, event := range events {
		tokens, err := adapter.Feed(event.EventType, event.Data)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if isStream && !bufferText {
			for _, token := range tokens {
				frames = append(frames, consoleResponseTextDeltaFrame(messageID, token))
			}
		}
	}

	fullText := adapter.FullText()
	toolCalls := adapter.ParsedToolCalls()
	toolItems := buildResponseFunctionCallItems(toolCalls)
	usage := consoleResponseUsageForOutput(adapter, options.Messages, fullText, toolItems)
	output := []map[string]any{}
	if len(toolItems) > 0 {
		output = append(output, toolItems...)
	} else {
		output = append(output, consoleResponseOutputItem(messageID, fullText))
	}
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
	if len(toolItems) > 0 {
		frames = append(frames, emitResponseFunctionCallEvents(toolItems, 0)...)
		frames = append(frames, FormatSSE("response.completed", map[string]any{
			"type":     "response.completed",
			"response": response,
		}), "data: [DONE]\n\n")
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}
	if bufferText {
		frames = append(frames, consoleResponseMessageStartFrames(messageID, 0)...)
		if fullText != "" {
			frames = append(frames, consoleResponseTextDeltaFrame(messageID, fullText))
		}
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
	frames := consoleResponseLifecycleFrames(responseID, modelName, isStream)
	if !isStream {
		return frames
	}
	return append(frames, consoleResponseMessageStartFrames(messageID, 0)...)
}

func consoleResponseLifecycleFrames(responseID, modelName string, isStream bool) []string {
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
	}
}

func consoleResponseMessageStartFrames(messageID string, outputIndex int) []string {
	return []string{
		FormatSSE("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": outputIndex,
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
			"output_index":  outputIndex,
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
	return consoleResponseUsageForOutput(adapter, messages, adapter.FullText(), nil)
}

func consoleResponseUsageForOutput(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any, text string, toolItems []map[string]any) map[string]any {
	if adapter.Usage != nil {
		return BuildRespUsage(
			intFromAny(adapter.Usage["input_tokens"]),
			intFromAny(adapter.Usage["output_tokens"]),
		)
	}
	outputTokens := platform.EstimateTokens(text)
	if len(toolItems) > 0 {
		outputTokens = estimateToolCallTokens(toolItems)
	}
	return BuildRespUsage(
		platform.EstimatePromptTokens(messages, platform.PromptOverhead),
		outputTokens,
	)
}
