package openai

import (
	"context"
	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/platform"
)

func GenerateImages(ctx context.Context, options imageGenerationOptions) (imageResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return imageResult{}, err
	}
	if options.Model == "grok-imagine-image-lite" {
		return generateLiteImages(ctx, options)
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return imageResult{}, platform.NewRateLimitError("Account directory not initialised")
	}

	account, ok, err := directory.ReserveChatAccount(ctx, spec, nil)
	if err != nil {
		return imageResult{}, err
	}
	if !ok {
		return imageResult{}, platform.NewRateLimitError("No available accounts for image generation")
	}

	responseID := imageResponseID()
	success := false
	var failErr error
	defer func() {
		_ = directory.ReleaseChatAccount(ctx, account)
		if success || failErr == nil {
			return
		}
		kind := feedbackKind(failErr)
		if kind == feedbackKindUnauthorized || kind == feedbackKindForbidden {
			_ = directory.FeedbackChatAccount(ctx, chatFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
		}
	}()

	finals, frames, reasoning, err := runImagineGeneration(ctx, options, account, responseID)
	if err != nil {
		failErr = err
		return imageResult{}, err
	}
	if len(finals) > 0 {
		success = true
	}
	if options.Stream {
		return imageResult{IsStream: true, StreamFrames: frames}, nil
	}
	if options.ChatFormat {
		return imageResult{Response: MakeChatResponse(ChatResponseParams{
			Model:            options.Model,
			Content:          joinImageOutputs(finals, true),
			PromptContent:    options.Prompt,
			ResponseID:       responseID,
			ReasoningContent: reasoning,
		})}, nil
	}
	data, err := imageResponseData(finals, options.ResponseFormat)
	if err != nil {
		return imageResult{}, err
	}
	return imageResult{Response: map[string]any{
		"created": imageNowUnix(),
		"data":    data,
	}}, nil
}
