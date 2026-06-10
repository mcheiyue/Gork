package openai

import (
	"context"
	"fmt"
	"strings"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	"github.com/jiujiu532/grok2api/app/platform"
)

func EditImages(ctx context.Context, options imageEditOptions) (imageResult, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return imageResult{}, err
	}
	n := options.N
	if n == 0 {
		n = 1
	}
	if n < 1 || n > 2 {
		return imageResult{}, platform.NewValidationError("image edit n must be between 1 and 2", "n", "")
	}
	if _, err := normalizeEditSize(options.Size); err != nil {
		return imageResult{}, err
	}
	prompt, imageInputs, err := extractEditPromptAndInputs(options.Messages)
	if err != nil {
		return imageResult{}, err
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
		return imageResult{}, platform.NewRateLimitError("No available accounts for image edit")
	}
	responseID := imageResponseID()

	released := false
	releaseOnly := func() {
		if !released {
			_ = directory.ReleaseChatAccount(ctx, account)
			released = true
		}
	}

	editReferences, err := prepareEditReferences(ctx, account.Token, imageInputs)
	if err != nil {
		releaseOnly()
		return imageResult{}, err
	}
	if len(editReferences) == 0 {
		releaseOnly()
		return imageResult{}, platform.NewUpstreamError("All image uploads failed; cannot proceed with image edit", 502, "")
	}

	editPrompt := replaceEditImagePlaceholders(prompt, editReferences)
	imageReferences := make([]string, 0, len(editReferences))
	for _, ref := range editReferences {
		imageReferences = append(imageReferences, ref.URL)
	}

	post, err := imageCreateMediaPost(ctx, account.Token, protocol.ImagePostMediaType, transport.MediaOptions{Prompt: editPrompt})
	if err != nil {
		releaseOnly()
		return imageResult{}, err
	}
	parentPostID, editPrompt, err := parseEditPost(post, editPrompt)
	if err != nil {
		releaseOnly()
		return imageResult{}, err
	}

	frames := []string{}
	tracker := newImageProgressTracker(n)
	images, err := imageCollectEditImages(ctx, imageCollectEditOptions{
		Token:           account.Token,
		Prompt:          editPrompt,
		ImageReferences: imageReferences,
		ParentPostID:    parentPostID,
		RequestedN:      n,
		ResponseFormat:  options.ResponseFormat,
		ProgressCB: func(index int, progressValue int) {
			reason, advanced := tracker.Record(index, progressValue)
			if advanced && options.Stream && options.ChatFormat {
				frames = appendImageThinkingFrame(frames, options.Model, responseID, reason)
			}
		},
	})
	finishChatAttempt(ctx, directory, account, err == nil, err)
	released = true
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
			PromptContent:    prompt,
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

func prepareEditReferences(ctx context.Context, token string, imageInputs []string) ([]editReference, error) {
	references := make([]editReference, 0, len(imageInputs))
	for index, imageInput := range imageInputs {
		uploaded, err := imageUploadFromInput(ctx, token, imageInput)
		if err != nil {
			var validation *platform.ValidationError
			if errorsAs(err, &validation) {
				return nil, platform.NewValidationError(validation.Message, fmt.Sprintf("image.%d", index+1), "")
			}
			return nil, platform.NewUpstreamError(fmt.Sprintf("Image edit reference %d upload failed: %v", index+1, err), 502, "")
		}
		contentURL, err := imageResolveUploadedAssetReference(token, uploaded.FileID, uploaded.FileURI)
		if err != nil {
			return nil, err
		}
		references = append(references, editReference{
			FileID:  uploaded.FileID,
			FileURI: uploaded.FileURI,
			URL:     contentURL,
		})
	}
	return references, nil
}

func parseEditPost(post map[string]any, fallbackPrompt string) (string, string, error) {
	postData, ok := post["post"].(map[string]any)
	if !ok {
		return "", "", platform.NewUpstreamError("Image edit create-post returned no post payload", 502, "")
	}
	parentPostID := strings.TrimSpace(stringValue(postData["id"], ""))
	if parentPostID == "" {
		return "", "", platform.NewUpstreamError("Image edit create-post returned no post id", 502, "")
	}
	if postPrompt := strings.TrimSpace(stringValue(postData["originalPrompt"], "")); postPrompt != "" {
		return parentPostID, postPrompt, nil
	}
	if postPrompt := strings.TrimSpace(stringValue(postData["prompt"], "")); postPrompt != "" {
		return parentPostID, postPrompt, nil
	}
	return parentPostID, fallbackPrompt, nil
}
