package openai

import (
	"context"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	"github.com/jiujiu532/grok2api/app/platform"
)

func runImagineGeneration(ctx context.Context, options imageGenerationOptions, account chatAccount, responseID string) ([]imageOutput, []string, string, error) {
	n := options.N
	if n <= 0 {
		n = 1
	}
	size := options.Size
	if size == "" {
		size = "1024x1024"
	}
	aspectRatio := ResolveAspectRatio(size)
	enableNSFW := imageEnableNSFW()
	events, err := imageStreamImages(ctx, account.Token, options.Prompt, transport.ImagineOptions{
		AspectRatio: aspectRatio,
		N:           n,
		EnableNSFW:  &enableNSFW,
		EnablePro:   options.Model == "grok-imagine-image-pro",
	})
	if err != nil {
		return nil, nil, "", err
	}

	finals := []imageOutput{}
	frames := []string{}
	tracker := newImageProgressTracker(n)
	for _, event := range events {
		eventType := stringValue(event["type"], "")
		switch eventType {
		case "error":
			return nil, nil, "", platform.NewUpstreamError("Image generation failed: "+stringValue(event["error"], "unknown"), 502, "")
		case "moderated":
			continue
		case "progress":
			key := imageEventKey(event, "progress", len(tracker.progressMap))
			reason, advanced := tracker.Record(key, imageProgressInt(event["progress"]))
			if advanced && options.Stream && options.ChatFormat {
				frames = appendImageThinkingFrame(frames, options.Model, responseID, reason)
			}
			continue
		}
		if !imageBool(event["is_final"]) {
			continue
		}
		key := imageEventKey(event, "final", len(tracker.completedIDs))
		reason, advanced := tracker.Record(key, 100)
		if advanced && options.Stream && options.ChatFormat {
			frames = appendImageThinkingFrame(frames, options.Model, responseID, reason)
		}
		image, err := resolveImageOutput(ctx, imageOutputOptions{
			Token:          account.Token,
			URL:            stringValue(event["url"], ""),
			ResponseFormat: options.ResponseFormat,
			BlobB64:        stringValue(event["blob"], ""),
		})
		if err != nil {
			return nil, nil, "", err
		}
		finals = append(finals, image)
		if options.Stream {
			content := outputContent(image, options.ChatFormat)
			frames = append(frames, formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
				ResponseID: responseID,
				Model:      options.Model,
				Content:    content,
			})))
		}
	}
	if options.Stream {
		frames = append(frames,
			formatChatDataFrame(MakeStreamChunk(StreamChunkParams{
				ResponseID:   responseID,
				Model:        options.Model,
				Content:      "",
				IsFinal:      true,
				FinishReason: "stop",
			})),
			"data: [DONE]\n\n",
		)
	}
	return finals, frames, tracker.ReasoningContent(), nil
}
