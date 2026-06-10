package openai

import "fmt"

func BuildRespUsage(inputTokens, outputTokens int, reasoningTokens ...int) map[string]any {
	return map[string]any{
		"input_tokens":  maxInt(0, inputTokens),
		"output_tokens": maxInt(0, outputTokens),
		"total_tokens":  maxInt(0, inputTokens+outputTokens),
		"output_tokens_details": map[string]any{
			"reasoning_tokens": optionalNonNegative(reasoningTokens),
		},
	}
}

func MakeRespObject(params RespObjectParams) map[string]any {
	object := map[string]any{
		"id":         params.ResponseID,
		"object":     "response",
		"created_at": formatNowUnix(),
		"status":     params.Status,
		"model":      params.Model,
		"output":     params.Output,
	}
	if params.Usage != nil {
		object["usage"] = params.Usage
	}
	return object
}

func FormatSSE(event string, data any) string {
	payload, err := marshalCompactJSON(data)
	if err != nil {
		payload = "null"
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, payload)
}
