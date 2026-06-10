package openai

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
)

func isImaginePublicURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.HasPrefix(parsed.Hostname(), "imagine-public")
}

func normalizeImageFormat(value string) string {
	format := strings.ToLower(strings.TrimSpace(value))
	if format == "" {
		format = "grok_url"
	}
	switch format {
	case "grok_url", "local_url", "grok_md", "local_md", "base64":
		return format
	default:
		panic("image_format must be one of [grok_url, local_url, grok_md, local_md, base64]")
	}
}

func resolveImage(ctx context.Context, token string, rawURL string, imageID string) (string, error) {
	format := normalizeImageFormat(imageFormatConfig)
	proxyImaginePublic := isImaginePublicURL(rawURL) && proxyImaginePublicConfig

	if format == "grok_url" && !proxyImaginePublic {
		return rawURL, nil
	}
	if format == "grok_md" && !proxyImaginePublic {
		return "![image](" + rawURL + ")", nil
	}

	raw, mime, err := downloadImageBytes(ctx, token, rawURL)
	if err != nil {
		return rawURL, nil
	}
	if mime == "" {
		mime = "image/jpeg"
	}

	if format == "base64" {
		return "![image](data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw) + ")", nil
	}

	fileID := saveImage(raw, mime, imageID)
	appURL := strings.TrimRight(appURLConfig, "/")
	localURL := "/v1/files/image?id=" + fileID
	if appURL != "" {
		localURL = appURL + localURL
	}
	if format == "grok_url" || format == "local_url" {
		return localURL, nil
	}
	return "![image](" + localURL + ")", nil
}

func prepareFileAttachments(ctx context.Context, token string, fileInputs []string) ([]string, error) {
	attachments := []string{}
	for _, fileInput := range fileInputs {
		if fileInput == "" {
			continue
		}
		fileID, _, err := uploadInput(ctx, token, fileInput)
		if err != nil {
			return nil, err
		}
		if fileID != "" {
			attachments = append(attachments, fileID)
		}
	}
	return attachments, nil
}
