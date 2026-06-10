package openai

import (
	"context"
	"encoding/json"
	"fmt"
	proxyadapters "github.com/jiujiu532/grok2api/app/dataplane/proxy/adapters"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
	"regexp"
	"sort"
	"strings"
)

var imageXUserIDPattern = regexp.MustCompile(`(?:^|;\s*)x-userid=([^;]+)`)

func collectEditImages(ctx context.Context, options imageCollectEditOptions) ([]imageOutput, error) {
	requested := options.RequestedN
	if requested <= 0 {
		requested = 1
	}
	seen := map[string]struct{}{}
	images := []imageOutput{}
	for attempt := 0; attempt < 2 && len(images) < requested; attempt++ {
		finalURLs, err := collectEditFinalURLs(ctx, options)
		if err != nil {
			return nil, err
		}
		indexes := make([]int, 0, len(finalURLs))
		for index := range finalURLs {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			rawURL := finalURLs[index]
			if rawURL == "" {
				continue
			}
			if _, exists := seen[rawURL]; exists {
				continue
			}
			seen[rawURL] = struct{}{}
			image, err := resolveImageOutput(ctx, imageOutputOptions{
				Token:          options.Token,
				URL:            rawURL,
				ResponseFormat: options.ResponseFormat,
			})
			if err != nil {
				return nil, err
			}
			images = append(images, image)
			if len(images) >= requested {
				break
			}
		}
	}
	return images, nil
}

func streamImageEditLines(ctx context.Context, options imageCollectEditOptions) ([]string, error) {
	payload := protocol.BuildImageEditPayload(protocol.ImageEditPayloadOptions{
		Prompt:          options.Prompt,
		ImageReferences: options.ImageReferences,
		ParentPostID:    options.ParentPostID,
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
			"referer":       "https://grok.com/imagine/post/" + options.ParentPostID,
		},
		PayloadBytes:   payloadBytes,
		TimeoutSeconds: chatTimeoutSeconds(),
	})
	if err != nil {
		return nil, transportUpstreamError(err, "Image-edit transport failed")
	}
	if response == nil {
		return nil, platform.NewUpstreamError("Image-edit upstream returned 502", 502, "")
	}
	if response.StatusCode != 200 {
		body := response.Body
		if len(body) > 300 {
			body = body[:300]
		}
		return nil, platform.NewUpstreamError(fmt.Sprintf("Image-edit upstream returned %d", response.StatusCode), response.StatusCode, body)
	}
	return append([]string{}, response.Lines...), nil
}

func collectEditFinalURLs(ctx context.Context, options imageCollectEditOptions) (map[int]string, error) {
	lines, err := imageStreamEditLines(ctx, options)
	if err != nil {
		return nil, err
	}
	finalURLs := map[int]string{}
	userID := imageExtractUserID(options.Token)
	for _, line := range lines {
		data, ok := imageSSEData(line)
		if !ok {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			continue
		}
		if err := imageStreamError(obj); err != nil {
			return nil, err
		}
		collectEditResults(obj, finalURLs, userID, options.ProgressCB)
	}
	if len(finalURLs) == 0 {
		return nil, platform.NewUpstreamError("Image edit returned no images", 502, "")
	}
	return finalURLs, nil
}

func collectEditResults(obj map[string]any, finalURLs map[int]string, userID string, progressCB func(index int, progress int)) {
	if stream := protocol.ExtractStreamingImageEditResponse(obj); stream != nil {
		index := 0
		if parsed := parseImageIndex(stream["imageIndex"]); parsed != nil {
			index = *parsed
			if progressCB != nil {
				progressCB(index, imageProgressInt(stream["progress"]))
			}
		}
		progress := imageProgressInt(stream["progress"])
		if progress >= 100 && !imageBool(stream["moderated"]) {
			if finalURL := resolveEditFinalURL(stringValue(stream["imageUrl"], ""), stringValue(stream["assetId"], ""), userID); finalURL != "" {
				finalURLs[index] = finalURL
			}
		}
	}
	for index, rawURL := range protocol.ExtractModelResponseImageEditURLs(obj) {
		if _, exists := finalURLs[index]; !exists {
			finalURLs[index] = absolutizeAssetURL(rawURL)
		}
	}
	for index, assetID := range protocol.ExtractModelResponseImageEditFileAttachments(obj) {
		if _, exists := finalURLs[index]; !exists {
			if finalURL := resolveEditFinalURL("", assetID, userID); finalURL != "" {
				finalURLs[index] = finalURL
			}
		}
	}
}

func imageSSEData(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", false
	}
	if trimmed == "[DONE]" {
		return "[DONE]", true
	}
	if !strings.HasPrefix(trimmed, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")), true
}

func imageStreamError(obj map[string]any) error {
	if errObj, ok := obj["error"].(map[string]any); ok {
		message := stringValue(errObj["message"], "")
		if message == "" {
			message = fmt.Sprint(errObj)
		}
		return platform.NewUpstreamError(message, 502, "")
	}
	if message := stringValue(obj["error"], ""); message != "" {
		return platform.NewUpstreamError(message, 502, "")
	}
	return nil
}

func resolveEditFinalURL(rawURL, assetID, userID string) string {
	if assetID != "" {
		if resolved := protocol.ResolveAssetReference(assetID, "", userID); resolved != nil {
			return *resolved
		}
	}
	if rawURL == "" {
		return ""
	}
	return absolutizeAssetURL(rawURL)
}

func absolutizeAssetURL(rawURL string) string {
	fullURL, _, _ := protocol.ResolveDownloadURL(rawURL)
	return fullURL
}

func imageExtractUserID(token string) string {
	match := imageXUserIDPattern.FindStringSubmatch(proxyadapters.BuildSSOCookie(token))
	if len(match) > 1 {
		return match[1]
	}
	return ""
}
