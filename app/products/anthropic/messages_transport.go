package anthropic

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	reverseruntime "github.com/jiujiu532/grok2api/app/dataplane/reverse/runtime"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
)

var messagesUploadInput = func(ctx context.Context, token string, fileInput string) (string, string, error) {
	result, err := transport.UploadFromInput(ctx, token, fileInput)
	if err != nil {
		return "", "", err
	}
	return result.FileID, result.FileURI, nil
}

func defaultMessagesStream(ctx context.Context, options messagesStreamOptions) ([]string, error) {
	attachments, err := prepareMessagesFileAttachments(ctx, options.Token, options.Files)
	if err != nil {
		return nil, err
	}
	payload := protocol.BuildChatPayload(protocol.ChatPayloadOptions{
		Message:         options.Message,
		ModeID:          options.ModeID,
		FileAttachments: attachments,
	})
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return postMessagesStream(ctx, options, payloadBytes)
}

func prepareMessagesFileAttachments(ctx context.Context, token string, fileInputs []string) ([]string, error) {
	attachments := []string{}
	for _, fileInput := range fileInputs {
		if fileInput == "" {
			continue
		}
		fileID, _, err := messagesUploadInput(ctx, token, fileInput)
		if err != nil {
			return nil, err
		}
		attachments = appendNonEmpty(attachments, fileID)
	}
	return attachments, nil
}

func postMessagesStream(ctx context.Context, options messagesStreamOptions, payload []byte) ([]string, error) {
	stream, err := transport.PostStream(ctx, reverseruntime.Chat, options.Token, payload, messagesHTTPOptions(options.TimeoutSeconds))
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return lines, nil
		}
		lines = append(lines, line)
	}
}

func messagesHTTPOptions(timeoutSeconds float64) transport.HTTPOptions {
	timeout := time.Duration(timeoutSeconds * float64(time.Second))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return transport.HTTPOptions{
		Timeout:     timeout,
		ContentType: "application/json",
		Origin:      reverseruntime.Base,
		Referer:     reverseruntime.Base + "/",
	}
}
