package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func streamChat(ctx context.Context, options chatStreamOptions) ([]string, error) {
	attachments, err := prepareFileAttachments(ctx, options.Token, options.Files)
	if err != nil {
		return nil, err
	}

	payload := protocol.BuildChatPayload(protocol.ChatPayloadOptions{
		Message:             options.Message,
		ModeID:              options.ModeID,
		FileAttachments:     attachments,
		ToolOverrides:       options.ToolOverrides,
		ModelConfigOverride: options.ModelConfigOverride,
		RequestOverrides:    options.RequestOverrides,
	})
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	response, err := streamPost(ctx, chatStreamRequest{
		Token: options.Token,
		Headers: map[string]string{
			"authorization": "Bearer " + options.Token,
			"content-type":  "application/json",
			"origin":        "https://grok.com",
			"referer":       "https://grok.com/",
		},
		PayloadBytes:   payloadBytes,
		TimeoutSeconds: options.TimeoutSeconds,
	})
	if err != nil {
		return nil, transportUpstreamError(err, "Chat transport failed")
	}
	if response == nil {
		return nil, platform.NewUpstreamError("Chat upstream returned 502", 502, "")
	}
	if response.StatusCode != 200 {
		body := response.Body
		if len(body) > 400 {
			body = body[:400]
		}
		return nil, platform.NewUpstreamError(fmt.Sprintf("Chat upstream returned %d", response.StatusCode), response.StatusCode, body)
	}
	return append([]string{}, response.Lines...), nil
}
