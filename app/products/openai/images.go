package openai

import (
	"context"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
)

// GenerateImages 生成图片；非 lite 路径在上游可重试错误时换号继续（对齐 chenyme #616）。
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

	maxRetries := chatSelectionMaxRetries()
	retryCodes := configuredRetryCodes(chatRetryConfig())
	excluded := []string{}
	var lastErr error
	responseID := imageResponseID()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		account, ok, err := directory.ReserveChatAccount(ctx, spec, excluded)
		if err != nil {
			return imageResult{}, err
		}
		if !ok {
			if lastErr != nil {
				return imageResult{}, lastErr
			}
			return imageResult{}, platform.NewRateLimitError("No available accounts for image generation")
		}

		finals, frames, reasoning, err := runImagineGeneration(ctx, options, account, responseID)
		if err == nil && len(finals) == 0 {
			err = platform.NewUpstreamError("image generation returned no final images", 502, "")
		}
		finishChatAttempt(ctx, directory, account, err == nil, err)
		if err == nil {
			return formatImageResult(options, finals, frames, reasoning, responseID)
		}
		lastErr = err
		if shouldRetryUpstream(err, retryCodes) && attempt < maxRetries {
			excluded = append(excluded, account.Token)
			continue
		}
		return imageResult{}, err
	}
	if lastErr != nil {
		return imageResult{}, lastErr
	}
	return imageResult{}, platform.NewRateLimitError("No available accounts for image generation")
}

func formatImageResult(
	options imageGenerationOptions,
	finals []imageOutput,
	frames []string,
	reasoning string,
	responseID string,
) (imageResult, error) {
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
