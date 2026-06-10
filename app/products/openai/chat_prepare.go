package openai

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func toChatAnnotations(annotations []map[string]any) []map[string]any {
	if len(annotations) == 0 {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(annotations))
	for _, annotation := range annotations {
		result = append(result, map[string]any{
			"type": "url_citation",
			"url_citation": map[string]any{
				"url":         annotation["url"],
				"title":       annotation["title"],
				"start_index": annotation["start_index"],
				"end_index":   annotation["end_index"],
			},
		})
	}
	return result
}

func parseRetryCodes(value any) map[int]struct{} {
	result := map[int]struct{}{}
	var parts []any

	switch typed := value.(type) {
	case string:
		for _, part := range strings.Split(typed, ",") {
			parts = append(parts, strings.TrimSpace(part))
		}
	case []any:
		parts = append(parts, typed...)
	case []string:
		for _, part := range typed {
			parts = append(parts, part)
		}
	case []int:
		for _, part := range typed {
			parts = append(parts, part)
		}
	default:
		return result
	}

	for _, part := range parts {
		text := strings.TrimSpace(fmt.Sprint(part))
		if text == "" || !isDigits(text) {
			continue
		}
		code, err := strconv.Atoi(text)
		if err == nil {
			result[code] = struct{}{}
		}
	}
	return result
}

func configuredRetryCodes(config map[string]any) map[int]struct{} {
	raw, ok := config["retry.on_codes"]
	if !ok || raw == nil {
		raw = "429,401,503"
		if legacy, exists := config["retry.retry_status_codes"]; exists {
			raw = legacy
		}
	}
	return parseRetryCodes(raw)
}

func prepareChatCompletion(options chatCompletionOptions) (chatCompletionPlan, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return chatCompletionPlan{}, err
	}

	isStream := chatFeatureStream()
	if options.Stream != nil {
		isStream = *options.Stream
	}
	emitThink := chatFeatureThinking()
	if options.EmitThink != nil {
		emitThink = *options.EmitThink
	}

	plan := chatCompletionPlan{
		Spec:             spec,
		IsStream:         isStream,
		EmitThink:        emitThink,
		IsConsole:        spec.IsConsoleChat(),
		MaxRetries:       chatSelectionMaxRetries(),
		RetryCodes:       configuredRetryCodes(chatRetryConfig()),
		ResponseID:       chatResponseID(),
		TimeoutSeconds:   chatTimeoutSeconds(),
		RequestOverrides: options.RequestOverrides,
	}
	if plan.IsConsole {
		return plan, nil
	}

	message, files := extractMessage(options.Messages)
	if strings.TrimSpace(message) == "" {
		return chatCompletionPlan{}, platform.NewUpstreamError("Empty message after extraction", 400, "")
	}
	plan.Message = message
	plan.Files = files

	if len(options.Tools) > 0 {
		plan.ToolNames = protocol.ExtractToolNames(options.Tools)
		toolPrompt := protocol.BuildToolSystemPrompt(options.Tools, options.ToolChoice)
		plan.Message = protocol.InjectIntoMessage(plan.Message, toolPrompt)
	}

	return plan, nil
}
