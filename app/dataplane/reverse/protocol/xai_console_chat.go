package protocol

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

var ConsoleModels = map[string]string{
	"grok-4.3-console":                     "grok-4.3",
	"grok-4.3-low":                         "grok-4.3",
	"grok-4.3-medium":                      "grok-4.3",
	"grok-4.3-high":                        "grok-4.3",
	"grok-4.20-0309-reasoning-console":     "grok-4.20-0309-reasoning",
	"grok-4.20-0309-console":               "grok-4.20-0309",
	"grok-4.20-0309-non-reasoning-console": "grok-4.20-0309-non-reasoning",
	"grok-4.20-multi-agent-console":        "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-low":            "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-medium":         "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-high":           "grok-4.20-multi-agent-0309",
	"grok-4.20-multi-agent-xhigh":          "grok-4.20-multi-agent-0309",
	"grok-build-console":                   "grok-build-0.1",
}

var consoleModelsWithReasoning = map[string]struct{}{
	"grok-4.3":                   {},
	"grok-4.20-multi-agent-0309": {},
}

var consoleModelFixedEffort = map[string]string{
	"grok-4.3-low":                 "low",
	"grok-4.3-medium":              "medium",
	"grok-4.3-high":                "high",
	"grok-4.20-multi-agent-low":    "low",
	"grok-4.20-multi-agent-medium": "medium",
	"grok-4.20-multi-agent-high":   "high",
	"grok-4.20-multi-agent-xhigh":  "xhigh",
}

var consoleModelMaxOutputTokens = map[string]int{
	"grok-4.20-multi-agent-0309": 2000000,
	"grok-build-0.1":             256000,
}

var consoleModelsWithSearchTools = map[string]struct{}{
	"grok-4.20-multi-agent-0309":   {},
	"grok-4.20-0309":               {},
	"grok-4.20-0309-reasoning":     {},
	"grok-4.20-0309-non-reasoning": {},
	"grok-4.3":                     {},
	"grok-build-0.1":               {},
}

var consoleEffortMap = map[string]string{
	"none":    "none",
	"minimal": "low",
	"low":     "low",
	"medium":  "medium",
	"high":    "high",
	"xhigh":   "xhigh",
}

type ConsolePayloadOptions struct {
	Messages        []map[string]any
	Model           string
	Temperature     float64
	TopP            float64
	ReasoningEffort string
	Stream          *bool
}

type ConsoleStreamAdapter struct {
	TextBuf []string
	Usage   map[string]any
	done    bool
}

func BuildConsolePayload(options ConsolePayloadOptions) map[string]any {
	temperature := options.Temperature
	if temperature == 0 {
		temperature = 0.7
	}
	topP := options.TopP
	if topP == 0 {
		topP = 0.95
	}
	stream := true
	if options.Stream != nil {
		stream = *options.Stream
	}
	inputItems := make([]map[string]any, 0, len(options.Messages))
	for _, message := range options.Messages {
		if item := consoleInputItem(message); item != nil {
			inputItems = append(inputItems, item)
		}
	}
	effort := consoleModelFixedEffort[options.Model]
	if effort == "" {
		effort = consoleEffortMap[options.ReasoningEffort]
		if effort == "" {
			effort = "medium"
		}
	}
	consoleModel := options.Model
	if mapped := ConsoleModels[options.Model]; mapped != "" {
		consoleModel = mapped
	}
	maxTokens := consoleModelMaxOutputTokens[consoleModel]
	if maxTokens == 0 {
		maxTokens = 1000000
	}
	payload := map[string]any{
		"model":             consoleModel,
		"input":             inputItems,
		"max_output_tokens": maxTokens,
		"temperature":       temperature,
		"top_p":             topP,
		"store":             false,
		"include":           []any{"reasoning.encrypted_content"},
		"stream":            stream,
	}
	if _, ok := consoleModelsWithReasoning[consoleModel]; ok {
		payload["reasoning"] = map[string]any{"effort": effort}
	}
	if _, ok := consoleModelsWithSearchTools[consoleModel]; ok {
		payload["tools"] = []map[string]any{
			{"type": "web_search", "enable_image_understanding": true},
			{"type": "x_search", "enable_video_understanding": true},
		}
		payload["tool_choice"] = "auto"
	}
	return payload
}

func NewConsoleStreamAdapter() *ConsoleStreamAdapter {
	return &ConsoleStreamAdapter{}
}

func (a *ConsoleStreamAdapter) Feed(eventType, data string) ([]string, error) {
	if a.done {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return nil, nil
	}
	switch eventType {
	case "response.output_text.delta":
		delta := stringFromAny(obj["delta"])
		if delta != "" {
			a.TextBuf = append(a.TextBuf, delta)
			return []string{delta}, nil
		}
	case "response.completed":
		if resp, ok := obj["response"].(map[string]any); ok {
			if usage, ok := resp["usage"].(map[string]any); ok {
				a.Usage = usage
			}
		}
		a.done = true
	case "error":
		message := stringFromAny(obj["message"])
		if message == "" {
			message = fmt.Sprint(obj)
		}
		return nil, platform.NewUpstreamError("Console API error: "+message, 502, "")
	}
	return nil, nil
}

func (a *ConsoleStreamAdapter) FullText() string {
	return strings.Join(a.TextBuf, "")
}

func ClassifyConsoleLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "skip", ""
	}
	if strings.HasPrefix(line, "event:") {
		return "event", strings.TrimSpace(line[6:])
	}
	if strings.HasPrefix(line, "data:") {
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			return "done", ""
		}
		return "data", data
	}
	return "skip", ""
}

func ConsoleSuccessFeedback() controlproxy.ProxyFeedback {
	feedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackSuccess)
	status := 200
	feedback.StatusCode = &status
	return feedback
}

func ConsoleTransportErrorFeedback() controlproxy.ProxyFeedback {
	return controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)
}

func ConsoleStatusFeedback(status int) controlproxy.ProxyFeedback {
	kind := controlproxy.ProxyFeedbackForbidden
	if status == 403 {
		kind = controlproxy.ProxyFeedbackChallenge
	} else if status == 429 {
		kind = controlproxy.ProxyFeedbackRateLimited
	} else if status >= 500 {
		kind = controlproxy.ProxyFeedbackUpstream5xx
	}
	feedback := controlproxy.NewProxyFeedback(kind)
	feedback.StatusCode = &status
	return feedback
}

func consoleInputItem(message map[string]any) map[string]any {
	role := stringFromAny(message["role"])
	apiRole := "user"
	if role == "system" || role == "developer" {
		apiRole = "system"
	} else if role == "assistant" {
		apiRole = "assistant"
	}
	blocks := consoleContentBlocks(message["content"])
	if len(blocks) == 0 {
		return nil
	}
	return map[string]any{"role": apiRole, "content": blocks}
}

func consoleContentBlocks(content any) []map[string]any {
	switch typed := content.(type) {
	case string:
		return []map[string]any{{"type": "input_text", "text": typed}}
	case []any:
		blocks := []map[string]any{}
		for _, raw := range typed {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch stringFromAny(block["type"]) {
			case "text":
				blocks = append(blocks, map[string]any{"type": "input_text", "text": stringFromAny(block["text"])})
			case "image_url":
				if imageURL, ok := block["image_url"].(map[string]any); ok {
					if url := stringFromAny(imageURL["url"]); url != "" {
						blocks = append(blocks, map[string]any{"type": "input_image", "image_url": url})
					}
				}
			default:
				text := stringFromAny(block["text"])
				if text == "" {
					text = consolePythonString(block)
				}
				blocks = append(blocks, map[string]any{"type": "input_text", "text": text})
			}
		}
		return blocks
	default:
		return []map[string]any{{"type": "input_text", "text": consolePythonString(content)}}
	}
}

func consolePythonString(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case string:
		return v
	case bool:
		if v {
			return "True"
		}
		return "False"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("'%s': %s", key, consolePythonLiteral(v[key])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprint(v)
	}
}

func consolePythonLiteral(value any) string {
	switch v := value.(type) {
	case string:
		escaped := strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `'`, `\'`)
		return "'" + escaped + "'"
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case map[string]any:
		return consolePythonString(v)
	default:
		return fmt.Sprint(v)
	}
}
