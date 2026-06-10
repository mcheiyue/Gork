package openai

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

func ResolveAspectRatio(size string) string {
	if value, ok := imageRatios[size]; ok {
		return value
	}
	return "2:3"
}

func normalizeImageResponseFormat(responseFormat string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" {
		format = "url"
	}
	switch format {
	case "url", "b64_json":
		return format, nil
	default:
		return "", platform.NewValidationError("response_format must be one of ['url', 'b64_json']", "response_format", "")
	}
}

func localImageURL(fileID string) string {
	return imageAppURL() + "/v1/files/image?id=" + fileID
}

func extractImageFileID(rawURL string) string {
	parts := strings.Split(rawURL, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}
		stem := strings.SplitN(part, ".", 2)[0]
		if stem != "" && stem != "image" && stem != "original" && stem != "thumbnail" {
			return stem
		}
	}
	sum := sha1.Sum([]byte(rawURL))
	return hex.EncodeToString(sum[:])[:32]
}

func resolveImageOutput(ctx context.Context, options imageOutputOptions) (imageOutput, error) {
	format, err := normalizeImageResponseFormat(options.ResponseFormat)
	if err != nil {
		return imageOutput{}, err
	}
	if format == "url" && isImaginePublicURL(options.URL) && !imagePublicProxyEnabled() {
		return imageOutput{APIValue: options.URL, MarkdownValue: fmt.Sprintf("![image](%s)", options.URL)}, nil
	}
	if format == "url" && imageAppURL() == "" {
		return imageOutput{APIValue: options.URL, MarkdownValue: fmt.Sprintf("![image](%s)", options.URL)}, nil
	}

	mime := inferImageContentType(options.URL)
	raw := []byte(nil)
	if options.BlobB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(options.BlobB64)
		if err != nil {
			return imageOutput{}, platform.NewUpstreamError("Invalid upstream image blob: "+err.Error(), 502, "")
		}
		raw = decoded
	} else {
		downloaded, downloadedMime, err := imageDownloadBytes(ctx, options.Token, options.URL)
		if err != nil {
			return imageOutput{}, err
		}
		raw = downloaded
		if downloadedMime != "" {
			mime = downloadedMime
		}
	}

	if format == "b64_json" {
		encoded := options.BlobB64
		if encoded == "" {
			encoded = base64.StdEncoding.EncodeToString(raw)
		}
		dataURI := fmt.Sprintf("data:%s;base64,%s", mime, encoded)
		return imageOutput{APIValue: encoded, MarkdownValue: fmt.Sprintf("![image](%s)", dataURI)}, nil
	}

	fileID, err := imageSaveLocal(raw, mime, extractImageFileID(options.URL))
	if err != nil {
		return imageOutput{}, err
	}
	localURL := localImageURL(fileID)
	return imageOutput{APIValue: localURL, MarkdownValue: fmt.Sprintf("![image](%s)", localURL)}, nil
}

func inferImageContentType(rawURL string) string {
	if contentType := protocol.InferContentType(rawURL); contentType != nil && *contentType != "" {
		return *contentType
	}
	return "image/jpeg"
}

func outputContent(image imageOutput, chatFormat bool) string {
	if chatFormat {
		return image.MarkdownValue
	}
	return image.APIValue
}

func normalizeEditInputs(imageInputs []string) ([]string, error) {
	cleaned := make([]string, 0, len(imageInputs))
	for _, item := range imageInputs {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return nil, platform.NewValidationError("Image edit requires at least one image_url content block", "messages", "")
	}
	if len(cleaned) > editMaxReferences {
		cleaned = cleaned[len(cleaned)-editMaxReferences:]
	}
	return cleaned, nil
}

func normalizeEditSize(size string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(size))
	if normalized == "" {
		normalized = editDefaultSize
	}
	if normalized != editDefaultSize {
		return "", platform.NewValidationError("image edit currently only supports size '1024x1024'", "size", "")
	}
	return editDefaultSize, nil
}

func replaceEditImagePlaceholders(prompt string, references []editReference) string {
	return editImagePlaceholderRE.ReplaceAllStringFunc(prompt, func(match string) string {
		parts := editImagePlaceholderRE.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		imageNumber, err := strconv.Atoi(parts[1])
		if err != nil || imageNumber < 1 || imageNumber > len(references) {
			return match
		}
		return "@" + references[imageNumber-1].FileID
	})
}

func extractEditPromptAndInputs(messages []map[string]any) (string, []string, error) {
	prompt := ""
	imageInputs := []string{}
	for _, msg := range messages {
		content := msg["content"]
		if text, ok := content.(string); ok {
			trimmed := strings.TrimSpace(text)
			if trimmed != "" {
				prompt = trimmed
			}
			continue
		}
		blocks, ok := content.([]any)
		if !ok {
			continue
		}
		for _, block := range blocks {
			mapped, ok := block.(map[string]any)
			if !ok {
				continue
			}
			switch mapped["type"] {
			case "text":
				text := strings.TrimSpace(stringValue(mapped["text"], ""))
				if text != "" {
					prompt = text
				}
			case "image_url":
				imageURL, ok := mapped["image_url"].(map[string]any)
				if !ok {
					continue
				}
				if rawURL := stringValue(imageURL["url"], ""); rawURL != "" {
					imageInputs = append(imageInputs, rawURL)
				}
			}
		}
	}
	if prompt == "" {
		return "", nil, platform.NewValidationError("Image edit requires a non-empty text prompt", "messages", "")
	}
	cleaned, err := normalizeEditInputs(imageInputs)
	if err != nil {
		return "", nil, err
	}
	return prompt, cleaned, nil
}

func imageBoolConfig(key string, defaultValue bool) bool {
	value := config.GetConfig(key, defaultValue)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case nil:
		return defaultValue
	default:
		return defaultValue
	}
}

func imageBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != "" && strings.TrimSpace(typed) != "0"
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return false
	}
}

func parseImageIndex(value any) *int {
	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case string:
		value, err := strconv.Atoi(typed)
		if err != nil {
			return nil
		}
		parsed = value
	default:
		return nil
	}
	if parsed < 0 {
		return nil
	}
	return &parsed
}
