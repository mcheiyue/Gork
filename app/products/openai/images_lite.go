package openai

import (
	"context"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func generateLiteImages(ctx context.Context, options imageGenerationOptions) (imageResult, error) {
	n := options.N
	if n <= 0 {
		n = 1
	}
	responseID := imageResponseID()
	frames := []string{}
	tracker := newImageProgressTracker(n)
	images, err := imageRunLiteBatch(ctx, imageLiteBatchOptions{
		Model:          options.Model,
		Prompt:         options.Prompt,
		N:              n,
		ResponseFormat: options.ResponseFormat,
		ProgressCB: func(index int, progressValue int) {
			reason, advanced := tracker.Record(index, progressValue)
			if advanced && options.Stream && options.ChatFormat {
				frames = appendImageThinkingFrame(frames, options.Model, responseID, reason)
			}
		},
	})
	if err != nil {
		return imageResult{}, err
	}
	if options.Stream {
		frames = appendImageStreamFrames(frames, images, options.Model, responseID, options.ChatFormat)
		return imageResult{IsStream: true, StreamFrames: frames}, nil
	}
	if options.ChatFormat {
		return imageResult{Response: MakeChatResponse(ChatResponseParams{
			Model:            options.Model,
			Content:          joinImageOutputs(images, true),
			PromptContent:    options.Prompt,
			ResponseID:       responseID,
			ReasoningContent: tracker.ReasoningContent(),
		})}, nil
	}
	data, err := imageResponseData(images, options.ResponseFormat)
	if err != nil {
		return imageResult{}, err
	}
	return imageResult{Response: map[string]any{
		"created": imageNowUnix(),
		"data":    data,
	}}, nil
}

func runLiteBatch(ctx context.Context, options imageLiteBatchOptions) ([]imageOutput, error) {
	n := options.N
	if n <= 0 {
		n = 1
	}
	images := make([]imageOutput, 0, n)
	for i := 0; i < n; i++ {
		image, err := runLiteRequest(ctx, options, i)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}

func runLiteRequest(ctx context.Context, options imageLiteBatchOptions, index int) (imageOutput, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return imageOutput{}, err
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return imageOutput{}, platform.NewRateLimitError("Account directory not initialised")
	}
	maxRetries := chatSelectionMaxRetries()
	retryCodes := configuredRetryCodes(chatRetryConfig())
	excluded := []string{}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		account, ok, err := directory.ReserveChatAccount(ctx, spec, excluded)
		if err != nil {
			return imageOutput{}, err
		}
		if !ok {
			return imageOutput{}, platform.NewRateLimitError("No available accounts for image generation")
		}
		image, err := runLiteAttempt(ctx, account, spec, options, index)
		finishChatAttempt(ctx, directory, account, err == nil, err)
		if err == nil {
			return image, nil
		}
		lastErr = err
		if shouldRetryUpstream(err, retryCodes) && attempt < maxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return imageOutput{}, err
	}
	if lastErr != nil {
		return imageOutput{}, lastErr
	}
	return imageOutput{}, platform.NewRateLimitError("No available accounts for image generation")
}

func runLiteAttempt(ctx context.Context, account chatAccount, spec model.ModelSpec, options imageLiteBatchOptions, index int) (imageOutput, error) {
	lines, err := imageStreamLiteGenerate(ctx, account.Token, options.Prompt, spec.ModeID)
	if err != nil {
		return imageOutput{}, err
	}
	adapter := protocol.NewStreamAdapter(protocol.StreamAdapterOptions{})
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
			return imageOutput{}, err
		}
		for _, event := range events {
			if event.Kind == "image_progress" {
				if options.ProgressCB != nil {
					options.ProgressCB(index, imageProgressInt(event.Content))
				}
				continue
			}
			if event.Kind != "image" || event.Content == "" {
				continue
			}
			if options.ProgressCB != nil {
				options.ProgressCB(index, 100)
			}
			return resolveImageOutput(ctx, imageOutputOptions{
				Token:          account.Token,
				URL:            event.Content,
				ResponseFormat: options.ResponseFormat,
			})
		}
	}
	return imageOutput{}, platform.NewUpstreamError("Image generation returned no images", 502, "")
}
