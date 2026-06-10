package openai

import (
	"context"
	"strings"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

type responseOptions struct {
	Model        string
	Input        any
	Instructions string
	Stream       bool
	EmitThink    bool
	Temperature  float64
	TopP         float64
	Tools        []map[string]any
	ToolChoice   any
}

func Responses(ctx context.Context, options responseOptions) (chatCompletionResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return chatCompletionResult{}, err
	}
	messages := responseMessages(options.Instructions, options.Input)
	if spec.IsConsoleChat() {
		stream := options.Stream
		emitThink := options.EmitThink
		return ConsoleResponses(ctx, consoleResponseOptions{
			Model:       options.Model,
			Messages:    messages,
			Stream:      &stream,
			EmitThink:   &emitThink,
			Temperature: options.Temperature,
			TopP:        options.TopP,
			ResponseID:  MakeRespID("resp"),
			ReasoningID: MakeRespID("rs"),
			MessageID:   MakeRespID("msg"),
		})
	}

	directory := chatDirectoryProvider()
	if directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}
	message, files := extractMessage(messages)
	if strings.TrimSpace(message) == "" {
		return chatCompletionResult{}, platform.NewUpstreamError("Empty message after extraction", 400, "")
	}
	toolNames := []string{}
	if len(options.Tools) > 0 {
		chatTools := toResponseChatTools(options.Tools)
		toolNames = protocol.ExtractToolNames(chatTools)
		message = protocol.InjectIntoMessage(message, protocol.BuildToolSystemPrompt(chatTools, options.ToolChoice))
	}

	ids := responseIDs{
		ResponseID:  MakeRespID("resp"),
		ReasoningID: MakeRespID("rs"),
		MessageID:   MakeRespID("msg"),
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
		result, err := runResponseAttempt(ctx, responseAttemptOptions{
			Request:   options,
			Account:   account,
			Message:   message,
			Files:     files,
			IDs:       ids,
			ToolNames: toolNames,
		})
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

type responseIDs struct {
	ResponseID  string
	ReasoningID string
	MessageID   string
}

type responseAttemptOptions struct {
	Request   responseOptions
	Account   chatAccount
	Message   string
	Files     []string
	IDs       responseIDs
	ToolNames []string
}

func runResponseAttempt(ctx context.Context, options responseAttemptOptions) (chatCompletionResult, error) {
	lines, err := streamChat(ctx, chatStreamOptions{
		Token:          options.Account.Token,
		ModeID:         options.Account.ModeID,
		Message:        options.Message,
		Files:          options.Files,
		TimeoutSeconds: chatTimeoutSeconds(),
	})
	if err != nil {
		return chatCompletionResult{}, err
	}

	collected, frames, err := collectResponseStream(ctx, lines, options)
	if err != nil {
		return chatCompletionResult{}, err
	}
	output := responseOutputItems(options.IDs, collected.State, options.ToolNames, collected.ToolItems)
	outputTokens := estimateResponseOutputTokens(output, collected.State)
	usage := BuildRespUsage(
		platform.EstimatePromptTokens(options.Message, platform.PromptOverhead),
		outputTokens+platform.EstimateTokens(collected.State.Thinking),
		platform.EstimateTokens(collected.State.Thinking),
	)
	response := MakeRespObject(RespObjectParams{
		ResponseID: options.IDs.ResponseID,
		Model:      options.Request.Model,
		Status:     "completed",
		Output:     output,
		Usage:      usage,
	})
	if !options.Request.Stream {
		return chatCompletionResult{Response: response}, nil
	}
	frames = append(frames, FormatSSE("response.completed", map[string]any{
		"type":     "response.completed",
		"response": response,
	}), "data: [DONE]\n\n")
	return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
}
