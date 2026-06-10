package openai

import (
	"context"
	"github.com/jiujiu532/grok2api/app/platform"
)

func Completions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
	plan, err := prepareChatCompletion(options)
	if err != nil {
		return chatCompletionResult{}, err
	}
	if plan.IsConsole {
		stream := plan.IsStream
		emitThink := plan.EmitThink
		options.Stream = &stream
		options.EmitThink = &emitThink
		return consoleCompletions(ctx, options)
	}

	directory := chatDirectoryProvider()
	if directory == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Account directory not initialised")
	}

	excluded := []string{}
	var lastErr error
	for attempt := 0; attempt <= plan.MaxRetries; attempt++ {
		account, ok, err := directory.ReserveChatAccount(ctx, plan.Spec, excluded)
		if err != nil {
			return chatCompletionResult{}, err
		}
		if !ok {
			return chatCompletionResult{}, platform.NewRateLimitError("No available accounts for this model tier")
		}

		result, err := runChatCompletionAttempt(ctx, options, plan, account)
		finishChatAttempt(ctx, directory, account, err == nil, err)
		if err == nil {
			return result, nil
		}

		lastErr = err
		if shouldRetryUpstream(err, plan.RetryCodes) && attempt < plan.MaxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return chatCompletionResult{}, err
	}
	if lastErr != nil {
		return chatCompletionResult{}, lastErr
	}
	return chatCompletionResult{}, platform.NewRateLimitError("No available accounts for this model tier")
}

func runChatCompletionAttempt(ctx context.Context, options chatCompletionOptions, plan chatCompletionPlan, account chatAccount) (chatCompletionResult, error) {
	lines, err := streamChat(ctx, chatStreamOptions{
		Token:               account.Token,
		ModeID:              account.ModeID,
		Message:             plan.Message,
		Files:               plan.Files,
		ToolOverrides:       plan.ToolOverrides,
		ModelConfigOverride: nil,
		RequestOverrides:    plan.RequestOverrides,
		TimeoutSeconds:      plan.TimeoutSeconds,
	})
	if err != nil {
		return chatCompletionResult{}, err
	}

	state, frames, err := consumeChatLines(lines, consumeChatLinesOptions{
		Model:      options.Model,
		ResponseID: plan.ResponseID,
		EmitThink:  plan.EmitThink,
		IsStream:   plan.IsStream,
		ToolNames:  plan.ToolNames,
	})
	if err != nil {
		return chatCompletionResult{}, err
	}

	if plan.IsStream {
		return chatCompletionResult{IsStream: true, StreamFrames: frames}, nil
	}

	response, err := buildNonStreamChatResponse(chatResponseBuildOptions{
		Model:      options.Model,
		Message:    plan.Message,
		ResponseID: plan.ResponseID,
		ToolNames:  plan.ToolNames,
		EmitThink:  plan.EmitThink,
		State:      state,
	})
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{Response: response}, nil
}

func finishChatAttempt(ctx context.Context, directory chatDirectory, account chatAccount, success bool, err error) {
	_ = directory.ReleaseChatAccount(ctx, account)
	kind := feedbackKindSuccess
	if !success {
		kind = feedbackKind(err)
	}
	_ = directory.FeedbackChatAccount(ctx, chatFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
	if success {
		quotaSync(ctx, account.Token, int(account.ModeID))
	} else if err != nil {
		failSync(ctx, account.Token, int(account.ModeID), err)
	}
}
