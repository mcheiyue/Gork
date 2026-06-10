package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
)

type videoGenerateOptions struct {
	Model           string
	Prompt          string
	Seconds         int
	Size            string
	ResolutionName  string
	Preset          string
	InputReferences []map[string]any
	ProgressCB      func(int)
}

type videoCompletionOptions struct {
	Model          string
	Messages       []map[string]any
	Stream         bool
	Seconds        int
	Size           string
	ResolutionName string
	Preset         string
}

type resolvedVideoCompletion struct {
	Prompt         string
	References     []map[string]any
	ResolutionName string
	Preset         string
}

func VideoCompletions(ctx context.Context, options videoCompletionOptions) (chatCompletionResult, error) {
	resolved, err := resolveVideoCompletion(options)
	if err != nil {
		return chatCompletionResult{}, err
	}
	progress := []int{}
	artifact, err := videoGenerate(ctx, videoGenerateOptions{
		Model:           options.Model,
		Prompt:          resolved.Prompt,
		Seconds:         options.Seconds,
		Size:            options.Size,
		ResolutionName:  resolved.ResolutionName,
		Preset:          resolved.Preset,
		InputReferences: resolved.References,
		ProgressCB: func(value int) {
			progress = append(progress, value)
		},
	})
	if err != nil {
		return chatCompletionResult{}, err
	}
	content := artifact.VideoURL
	if artifact.LocalContentFilePath != "" {
		content = artifact.LocalContentFilePath
	}
	if options.Stream {
		return videoStreamResult(options.Model, content, progress), nil
	}
	return chatCompletionResult{Response: MakeChatResponse(ChatResponseParams{
		Model:            options.Model,
		Content:          content,
		PromptContent:    resolved.Prompt,
		ResponseID:       MakeResponseID(),
		ReasoningContent: videoReasoning(progress),
	})}, nil
}

func resolveVideoCompletion(options videoCompletionOptions) (resolvedVideoCompletion, error) {
	if err := ValidateVideoLength(options.Seconds); err != nil {
		return resolvedVideoCompletion{}, err
	}
	_, defaultResolution, err := resolveVideoSize(options.Size)
	if err != nil {
		return resolvedVideoCompletion{}, err
	}
	resolution, err := resolveVideoResolutionName(options.ResolutionName, defaultResolution)
	if err != nil {
		return resolvedVideoCompletion{}, err
	}
	preset, err := resolveVideoPreset(options.Preset, "custom")
	if err != nil {
		return resolvedVideoCompletion{}, err
	}
	prompt, references, err := extractVideoPromptAndReferences(options.Messages)
	return resolvedVideoCompletion{prompt, references, resolution, preset}, err
}

func videoStreamResult(modelName string, content string, progress []int) chatCompletionResult {
	responseID := MakeResponseID()
	frames := make([]string, 0, len(progress)+3)
	for _, value := range progress {
		chunk := MakeThinkingChunk(ThinkingChunkParams{
			ResponseID: responseID, Model: modelName,
			Content: videoProgressReason(value) + "\n",
		})
		frames = append(frames, "data: "+mustCompactJSON(chunk)+"\n\n")
	}
	frames = append(frames, "data: "+mustCompactJSON(MakeStreamChunk(StreamChunkParams{
		ResponseID: responseID, Model: modelName, Content: content,
	}))+"\n\n")
	frames = append(frames, "data: "+mustCompactJSON(MakeStreamChunk(StreamChunkParams{
		ResponseID: responseID, Model: modelName, IsFinal: true,
	}))+"\n\n")
	frames = append(frames, "data: [DONE]\n\n")
	return chatCompletionResult{IsStream: true, StreamFrames: frames}
}

func videoReasoning(progress []int) string {
	parts := make([]string, 0, len(progress))
	seen := map[string]struct{}{}
	for _, value := range progress {
		reason := videoProgressReason(value)
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		parts = append(parts, reason)
	}
	return strings.Join(parts, "\n")
}

func extractVideoPromptAndReferences(messages []map[string]any) (string, []map[string]any, error) {
	var prompt string
	var referenceURLs []string
	for index := len(messages) - 1; index >= 0; index-- {
		prompt, blockReferences := extractVideoMessage(messages[index]["content"])
		if len(blockReferences) > 0 && len(referenceURLs) == 0 {
			referenceURLs = blockReferences
		}
		if prompt != "" {
			break
		}
	}
	if prompt == "" {
		return "", nil, platform.NewValidationError("Video prompt cannot be empty", "messages", "")
	}
	references := make([]map[string]any, 0, minInt(len(referenceURLs), 7))
	for index, url := range referenceURLs {
		if index >= 7 {
			break
		}
		references = append(references, map[string]any{"image_url": url})
	}
	return prompt, references, nil
}

func extractVideoMessage(content any) (string, []string) {
	if text, ok := content.(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	items, ok := content.([]any)
	if !ok {
		return "", nil
	}
	textParts := []string{}
	references := []string{}
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if block["type"] == "text" {
			if text := strings.TrimSpace(asString(block["text"])); text != "" {
				textParts = append(textParts, text)
			}
		} else if block["type"] == "image_url" {
			if url := imageURLString(block["image_url"]); url != "" {
				references = append(references, url)
			}
		}
	}
	return strings.Join(textParts, " "), references
}

func imageURLString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return strings.TrimSpace(asString(typed["url"]))
	default:
		return ""
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func mustCompactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
