package openai

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/logging"
)

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid JSON body", "body", ""))
		return
	}
	if err := validateChat(req); err != nil {
		writeRouterError(w, err)
		return
	}
	shouldStreamMedia, err := shouldStartChatMediaStream(req)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	if shouldStreamMedia {
		streamChatMediaResult(w, r, req)
		return
	}
	result, err := dispatchChatRequest(r, req)
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeChatResult(w, result)
}

func shouldStartChatMediaStream(req ChatCompletionRequest) (bool, error) {
	isStream := routerBoolConfig("features.stream", true)
	if req.Stream != nil {
		isStream = *req.Stream
	}
	if !isStream {
		return false, nil
	}
	spec, _ := model.Get(req.Model)
	if spec.IsImage() {
		cfg := req.ImageConfig
		if cfg == nil {
			cfg = &ImageConfig{}
		}
		n := routerIntDefault(cfg.N, 1)
		if err := validateImageN(req.Model, n, "image_config.n"); err != nil {
			return false, err
		}
		return true, nil
	}
	if spec.IsImageEdit() {
		cfg := req.ImageConfig
		if cfg == nil {
			cfg = &ImageConfig{}
		}
		n := routerIntDefault(cfg.N, 1)
		if err := validateImageEditN(n, "image_config.n"); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func streamChatMediaResult(w http.ResponseWriter, r *http.Request, req ChatCompletionRequest) {
	startRouterStream(w)
	heartbeatID := MakeResponseID()
	writeRouterStreamHeartbeat(w, req.Model, heartbeatID)

	done := make(chan chatMediaStreamOutcome, 1)
	go func() {
		result, err := dispatchChatRequest(r, req)
		done <- chatMediaStreamOutcome{result: result, err: err}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case outcome := <-done:
			if outcome.err != nil {
				logChatMediaStreamError(req.Model, outcome.err)
				writeRouterStreamError(w, outcome.err)
				return
			}
			writeRouterStreamFrames(w, outcome.result.StreamFrames)
			return
		case <-ticker.C:
			writeRouterStreamHeartbeat(w, req.Model, heartbeatID)
		case <-r.Context().Done():
			return
		}
	}
}

type chatMediaStreamOutcome struct {
	result chatCompletionResult
	err    error
}

func logChatMediaStreamError(modelName string, err error) {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		logging.Logger.Warn(
			"chat media stream failed",
			"model", modelName,
			"status", upstream.Status,
			"message", truncateStreamErrorText(upstream.Message, 400),
			"body_len", len(upstream.Body),
			"body_excerpt", truncateStreamErrorText(upstreamBodyExcerpt(upstream, 400), 400),
		)
		return
	}
	logging.Logger.Warn(
		"chat media stream failed",
		"model", modelName,
		"error", truncateStreamErrorText(err.Error(), 400),
	)
}

func truncateStreamErrorText(value string, limit int) string {
	if limit <= 0 {
		limit = 400
	}
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func dispatchChatRequest(r *http.Request, req ChatCompletionRequest) (chatCompletionResult, error) {
	isStream := routerBoolConfig("features.stream", true)
	if req.Stream != nil {
		isStream = *req.Stream
	}
	spec, _ := model.Get(req.Model)
	messages := routerMessagesToMaps(req.Messages)
	if spec.IsImageEdit() {
		return dispatchChatImageEdit(r, req, messages, isStream)
	}
	if spec.IsImage() {
		return dispatchChatImage(r, req, isStream)
	}
	if spec.IsVideo() {
		return dispatchChatVideo(r, req, messages, isStream)
	}
	return dispatchChatText(r, req, messages, isStream)
}

func dispatchChatImageEdit(r *http.Request, req ChatCompletionRequest, messages []map[string]any, isStream bool) (chatCompletionResult, error) {
	cfg := req.ImageConfig
	if cfg == nil {
		cfg = &ImageConfig{}
	}
	n := routerIntDefault(cfg.N, 1)
	if err := validateImageEditN(n, "image_config.n"); err != nil {
		return chatCompletionResult{}, err
	}
	imageResult, err := routerEditImages(r.Context(), imageEditOptions{
		Model: req.Model, Messages: messages, N: n,
		Size:           routerStringDefault(cfg.Size, "1024x1024"),
		ResponseFormat: routerStringDefault(cfg.ResponseFormat, "url"),
		Stream:         isStream, ChatFormat: true,
	})
	return chatFromImageResult(imageResult), err
}

func dispatchChatImage(r *http.Request, req ChatCompletionRequest, isStream bool) (chatCompletionResult, error) {
	cfg := req.ImageConfig
	if cfg == nil {
		cfg = &ImageConfig{}
	}
	n := routerIntDefault(cfg.N, 1)
	if err := validateImageN(req.Model, n, "image_config.n"); err != nil {
		return chatCompletionResult{}, err
	}
	imageResult, err := routerGenerateImages(r.Context(), imageGenerationOptions{
		Model: req.Model, Prompt: lastUserText(req.Messages), N: n,
		Size:           routerStringDefault(cfg.Size, "1024x1024"),
		ResponseFormat: routerStringDefault(cfg.ResponseFormat, "url"),
		Stream:         isStream, ChatFormat: true,
	})
	return chatFromImageResult(imageResult), err
}

func dispatchChatVideo(r *http.Request, req ChatCompletionRequest, messages []map[string]any, isStream bool) (chatCompletionResult, error) {
	cfg := req.VideoConfig
	if cfg == nil {
		cfg = &VideoConfig{}
	}
	return VideoCompletions(r.Context(), videoCompletionOptions{
		Model: req.Model, Messages: messages, Stream: isStream,
		Seconds:        routerIntDefault(cfg.Seconds, 6),
		Size:           routerStringDefault(cfg.Size, "720x1280"),
		ResolutionName: cfg.ResolutionName,
		Preset:         cfg.Preset,
	})
}

func dispatchChatText(r *http.Request, req ChatCompletionRequest, messages []map[string]any, isStream bool) (chatCompletionResult, error) {
	emitThink := (*bool)(nil)
	if req.ReasoningEffort != nil {
		value := *req.ReasoningEffort != "none"
		emitThink = &value
	}
	return routerCompletions(r.Context(), chatCompletionOptions{
		Model:          req.Model,
		Messages:       messages,
		Stream:         &isStream,
		EmitThink:      emitThink,
		Tools:          req.Tools,
		ToolChoice:     req.ToolChoice,
		ResponseFormat: req.ResponseFormat,
		Temperature:    routerFloatDefault(req.Temperature, 0.8),
		TopP:           routerFloatDefault(req.TopP, 0.95),
	})
}

func handleResponses(w http.ResponseWriter, r *http.Request) {
	var req ResponsesCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid JSON body", "body", ""))
		return
	}
	spec, ok := model.Get(req.Model)
	if !ok || !spec.Enabled {
		writeRouterError(w, platform.NewValidationError("Model '"+req.Model+"' does not exist or you do not have access to it.", "model", "model_not_found"))
		return
	}
	if routerInputEmpty(req.Input) {
		writeRouterError(w, platform.NewValidationError("input cannot be empty", "input", ""))
		return
	}
	isStream := routerBoolConfig("features.stream", true)
	if req.Stream != nil {
		isStream = *req.Stream
	}
	emitThink := routerBoolConfig("features.thinking", true)
	if req.Reasoning != nil {
		emitThink = req.Reasoning["effort"] != "none"
	}
	result, err := routerResponses(r.Context(), responseOptions{
		Model:        req.Model,
		Input:        req.Input,
		Instructions: req.Instructions,
		Stream:       isStream,
		EmitThink:    emitThink,
		Temperature:  routerFloatDefault(req.Temperature, 0.8),
		TopP:         routerFloatDefault(req.TopP, 0.95),
		Tools:        routerToolMaps(req.Tools),
		ToolChoice:   req.ToolChoice,
	})
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeChatResult(w, result)
}

func handleImageGenerations(w http.ResponseWriter, r *http.Request) {
	var req ImageGenerationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid JSON body", "body", ""))
		return
	}
	spec, ok := model.Get(req.Model)
	if !ok || !spec.Enabled || !spec.IsImage() {
		writeRouterError(w, platform.NewValidationError("Model '"+req.Model+"' is not an image model", "model", ""))
		return
	}
	n := routerIntDefault(req.N, 1)
	if err := validateImageN(req.Model, n, "n"); err != nil {
		writeRouterError(w, err)
		return
	}
	result, err := routerGenerateImages(r.Context(), imageGenerationOptions{
		Model:          req.Model,
		Prompt:         req.Prompt,
		N:              n,
		Size:           routerStringDefault(req.Size, "1024x1024"),
		ResponseFormat: routerStringDefault(req.ResponseFormat, "url"),
		Stream:         false,
		ChatFormat:     false,
	})
	if err != nil {
		writeRouterError(w, err)
		return
	}
	writeImageResult(w, result)
}

func chatFromImageResult(result imageResult) chatCompletionResult {
	return chatCompletionResult{
		IsStream:     result.IsStream,
		StreamFrames: result.StreamFrames,
		Response:     result.Response,
	}
}

func routerStringDefault(value string, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func routerIntDefault(value int, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

func lastUserText(messages []MessageItem) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != "user" {
			continue
		}
		if text, ok := message.Content.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

func routerToolMaps(values []any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]any); ok {
			result = append(result, item)
		}
	}
	return result
}

func routerInputEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
