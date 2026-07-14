package openai

import (
	"context"
	"log/slog"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/products"
)

var consoleStreamChat = func(ctx context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
	return products.StreamConsoleChat(ctx, token, payload, timeoutS)
}

func init() {
	consoleCompletions = ConsoleCompletions
}

func consoleReasoningEffort(emitThink *bool) string {
	if emitThink != nil && !*emitThink {
		return "none"
	}
	return "low"
}

func ConsoleCompletions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
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
	maxRetries := chatSelectionMaxRetries()
	retryCodes := configuredRetryCodes(chatRetryConfig())
	responseID := chatResponseID()
	timeoutS := chatTimeoutSeconds()
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

		tokenPrefix := account.Token
		if len(tokenPrefix) > 8 {
			tokenPrefix = tokenPrefix[:8] + "..."
		}
		slog.Info("console attempt", "attempt", attempt, "excluded_count", len(excluded), "token_prefix", tokenPrefix)

		result, err := runConsoleCompletionAttempt(ctx, options, account, responseID, isStream, timeoutS)
		finishChatAttempt(ctx, directory, account, err == nil, err)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// 429：解析 rps/rpm → 武装冷却 → 有限换号，禁止 60s+ 空转
		if isConsoleRateLimitError(err) {
			errBody := extract429Body(err)
			info := parseConsole429Info(errBody)
			cooldown := consoleCircuitBreaker.tripModel(cooldownKey, info)
			slog.Info("console 429 parsed",
				"per_second_hit", info.IsPerSecondHit, "per_minute_hit", info.IsPerMinuteHit,
				"ps_actual", info.PerSecondActual, "pm_actual", info.PerMinuteActual,
				"cooldown_sec", int(cooldown.Seconds()), "key", cooldownKey)

			var delay time.Duration
			allowRetry := false
			switch {
			case info.IsPerMinuteHit:
				// team RPM：换号基本无效，最多 1 次短间隔重试
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

func runConsoleCompletionAttempt(ctx context.Context, options chatCompletionOptions, account chatAccount, responseID string, isStream bool, timeoutS float64) (chatCompletionResult, error) {
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

	events, err := consoleStreamChat(ctx, account.Token, payload, timeoutS)
	if err != nil {
		return chatCompletionResult{}, err
	}

	emitThink := options.EmitThink == nil || *options.EmitThink

	adapter := protocol.NewConsoleStreamAdapterWithTools(clientToolNames)
	// When client function tools are active, hold text until we know whether the
	// turn produced a function_call (a mixed content+tool_calls turn must not
	// stream earlier text). Without client tools, stream text deltas live.
	bufferText := len(clientToolNames) > 0
	frames := []string{}
	for _, event := range events {
		tokens, err := adapter.Feed(event.EventType, event.Data)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if isStream && !bufferText {
			for _, token := range tokens {
				frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
					ResponseID: responseID,
					Model:      options.Model,
					Content:    token,
				})))
			}
		}
	}

	toolCalls := adapter.ParsedToolCalls()
	thinkingText := adapter.ThinkingText()
	references := adapter.ReferencesSuffix(true)
	searchSources := adapter.SearchSourcesList()
	annotations := adapter.AnnotationsList()
	usage := consoleUsage(adapter, options.Messages)

	if isStream {
		// Tool-call turn: emit tool_call chunks and finish with tool_calls.
		if len(toolCalls) > 0 {
			toolUsage := usage
			if adapter.Usage == nil {
				toolUsage = BuildUsage(
					platform.EstimatePromptTokens(options.Messages, platform.PromptOverhead),
					platform.EstimateToolCallTokens(parsedToolCallsToAny(toolCalls)),
				)
			}
			for i, call := range toolCalls {
				frames = append(frames, formatChatDataFrame(MakeToolCallChunk(ToolCallChunkParams{
					ResponseID: responseID,
					Model:      options.Model,
					Index:      i,
					CallID:     call.CallID,
					Name:       call.Name,
					Arguments:  call.Arguments,
					IsFirst:    true,
				})))
			}
			frames = append(frames, formatChatDataFrame(MakeToolCallDoneChunk(ToolCallDoneChunkParams{
				ResponseID: responseID,
				Model:      options.Model,
				Usage:      toolUsage,
			})), "data: [DONE]\n\n")
			return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
		}
		// No tool calls: flush buffered text (if buffering was on), then thinking,
		// references and the final stop chunk.
		if bufferText {
			if text := adapter.FullText(); text != "" {
				frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
					ResponseID: responseID,
					Model:      options.Model,
					Content:    text,
				})))
			}
		}
		if emitThink && thinkingText != "" {
			frames = append(frames, formatChatDataFrame(MakeThinkingChunk(ThinkingChunkParams{
				ResponseID: responseID,
				Model:      options.Model,
				Content:    thinkingText,
			})))
		}
		if references != "" {
			frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
				ResponseID: responseID,
				Model:      options.Model,
				Content:    references,
			})))
		}
		final := MakeStreamChunk(StreamChunkParams{
			ResponseID:   responseID,
			Model:        options.Model,
			Content:      "",
			IsFinal:      true,
			Usage:        usage,
			FinishReason: "stop",
			Annotations:  toChatAnnotations(annotations),
		})
		if len(searchSources) > 0 {
			final["search_sources"] = searchSources
		}
		frames = append(frames, formatChatDataFrame(final), "data: [DONE]\n\n")
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}

	// Non-stream tool-call response.
	if len(toolCalls) > 0 {
		anyCalls := parsedToolCallsToAny(toolCalls)
		toolUsage := usage
		if adapter.Usage == nil {
			toolUsage = BuildUsage(
				platform.EstimatePromptTokens(options.Messages, platform.PromptOverhead),
				platform.EstimateToolCallTokens(anyCalls),
			)
		}
		response := MakeToolCallResponse(ToolCallResponseParams{
			Model:         options.Model,
			ToolCalls:     anyCalls,
			PromptContent: options.Messages,
			ResponseID:    responseID,
			Usage:         toolUsage,
		})
		if len(searchSources) > 0 {
			response["search_sources"] = searchSources
		}
		return chatCompletionResult{Response: response}, nil
	}

	content := adapter.FullText() + references
	reasoningContent := ""
	if emitThink {
		reasoningContent = thinkingText
	}
	response := MakeChatResponse(ChatResponseParams{
		Model:            options.Model,
		Content:          content,
		PromptContent:    options.Messages,
		ResponseID:       responseID,
		ReasoningContent: reasoningContent,
		SearchSources:    searchSources,
		Annotations:      toChatAnnotations(annotations),
		Usage:            usage,
	})
	return chatCompletionResult{Response: response}, nil
}

func parsedToolCallsToAny(calls []protocol.ParsedToolCall) []any {
	out := make([]any, 0, len(calls))
	for i, call := range calls {
		out = append(out, map[string]any{
			"id":    call.CallID,
			"type":  "function",
			"index": i,
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return out
}

func consoleUsage(adapter *protocol.ConsoleStreamAdapter, messages []map[string]any) map[string]any {
	if adapter.Usage != nil {
		inputTokens := intFromAny(adapter.Usage["input_tokens"])
		outputTokens := intFromAny(adapter.Usage["output_tokens"])
		return BuildUsage(inputTokens, outputTokens)
	}
	return BuildUsage(platform.EstimatePromptTokens(messages, platform.PromptOverhead), platform.EstimateTokens(adapter.FullText()))
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
