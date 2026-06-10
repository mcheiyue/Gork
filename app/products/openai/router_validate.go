package openai

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"path/filepath"
	"strings"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/platform"
)

var (
	validChatRoles = map[string]struct{}{
		"developer": {},
		"system":    {},
		"user":      {},
		"assistant": {},
		"tool":      {},
	}
	validEfforts = map[string]struct{}{
		"none":    {},
		"minimal": {},
		"low":     {},
		"medium":  {},
		"high":    {},
		"xhigh":   {},
	}
)

func validateChat(req ChatCompletionRequest) error {
	spec, ok := model.Get(req.Model)
	if !ok || !spec.Enabled {
		return platform.NewValidationError(
			fmt.Sprintf("Model %q does not exist or you do not have access to it.", req.Model),
			"model",
			"model_not_found",
		)
	}
	if len(req.Messages) == 0 {
		return platform.NewValidationError("messages cannot be empty", "messages", "")
	}
	for index, message := range req.Messages {
		if _, ok := validChatRoles[message.Role]; !ok {
			return platform.NewValidationError("role must be one of ['assistant', 'developer', 'system', 'tool', 'user']", fmt.Sprintf("messages.%d.role", index), "")
		}
	}
	if req.Temperature != nil && (*req.Temperature < 0 || *req.Temperature > 2) {
		return platform.NewValidationError("temperature must be between 0 and 2", "temperature", "")
	}
	if req.TopP != nil && (*req.TopP < 0 || *req.TopP > 1) {
		return platform.NewValidationError("top_p must be between 0 and 1", "top_p", "")
	}
	if req.ReasoningEffort != nil {
		if _, ok := validEfforts[*req.ReasoningEffort]; !ok {
			return platform.NewValidationError("reasoning_effort must be one of ['high', 'low', 'medium', 'minimal', 'none', 'xhigh']", "reasoning_effort", "")
		}
	}
	return nil
}

func validateImageN(modelName string, n int, param string) error {
	maxN := 10
	if modelName == "grok-imagine-image-lite" {
		maxN = 4
	}
	if n < 1 || n > maxN {
		return platform.NewValidationError(fmt.Sprintf("n must be between 1 and %d for model %q", maxN, modelName), param, "")
	}
	return nil
}

func validateImageEditN(n int, param string) error {
	if n < 1 || n > 2 {
		return platform.NewValidationError("n must be between 1 and 2 for image edit", param, "")
	}
	return nil
}

func routerMessagesToMaps(messages []MessageItem) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		item := map[string]any{"role": message.Role}
		if message.Content != nil {
			item["content"] = message.Content
		}
		result = append(result, item)
	}
	return result
}

func uploadFileToDataURI(header *multipart.FileHeader, param string) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", platform.NewValidationError("Uploaded image cannot be empty", param, "")
	}
	mimeType := strings.TrimSpace(strings.ToLower(header.Header.Get("Content-Type")))
	if mimeType == "" {
		mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(header.Filename)))
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return "", platform.NewValidationError("Uploaded file must be an image", param, "")
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}
