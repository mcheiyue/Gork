package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

type ChatPayloadOptions struct {
	Message             string
	ModeID              controlmodel.ModeID
	FileAttachments     []string
	ToolOverrides       map[string]any
	ModelConfigOverride map[string]any
	RequestOverrides    map[string]any
	MemoryEnabled       bool
	Temporary           *bool
	CustomInstruction   string
}

type FrameEvent struct {
	Kind           string
	Content        string
	ImageID        string
	RolloutID      string
	MessageTag     string
	MessageStepID  *int
	AnnotationData map[string]any
}

func BuildChatPayload(options ChatPayloadOptions) map[string]any {
	temporary := true
	if options.Temporary != nil {
		temporary = *options.Temporary
	}
	toolOverrides := options.ToolOverrides
	if toolOverrides == nil {
		toolOverrides = map[string]any{
			"gmailSearch":           false,
			"googleCalendarSearch":  false,
			"outlookSearch":         false,
			"outlookCalendarSearch": false,
			"googleDriveSearch":     false,
		}
	}
	responseMetadata := map[string]any{}
	payload := map[string]any{
		"collectionIds":               []any{},
		"connectors":                  []any{},
		"deviceEnvInfo":               defaultDeviceEnvInfo(),
		"disableMemory":               !options.MemoryEnabled,
		"disableSearch":               false,
		"disableSelfHarmShortCircuit": false,
		"disableTextFollowUps":        false,
		"enableImageGeneration":       true,
		"enableImageStreaming":        true,
		"enableSideBySide":            true,
		"fileAttachments":             append([]string{}, options.FileAttachments...),
		"forceConcise":                false,
		"forceSideBySide":             false,
		"imageAttachments":            []any{},
		"imageGenerationCount":        2,
		"isAsyncChat":                 false,
		"message":                     options.Message,
		"modeId":                      options.ModeID.ToAPIString(),
		"responseMetadata":            responseMetadata,
		"returnImageBytes":            false,
		"returnRawGrokInXaiRequest":   false,
		"searchAllConnectors":         false,
		"sendFinalMetadata":           true,
		"temporary":                   temporary,
		"toolOverrides":               toolOverrides,
	}
	if strings.TrimSpace(options.CustomInstruction) != "" {
		payload["customPersonality"] = strings.TrimSpace(options.CustomInstruction)
	}
	if options.ModelConfigOverride != nil {
		responseMetadata["modelConfigOverride"] = options.ModelConfigOverride
	}
	for key, value := range options.RequestOverrides {
		if value != nil {
			payload[key] = value
		}
	}
	return payload
}

func ClassifyLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "skip", ""
	}
	if strings.HasPrefix(line, "data:") {
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			return "done", ""
		}
		return "data", data
	}
	if strings.HasPrefix(line, "event:") {
		return "skip", ""
	}
	if strings.HasPrefix(line, "{") {
		return "data", line
	}
	return "skip", ""
}

func StreamErrorFromPayload(obj map[string]any) *platform.UpstreamError {
	rawError, ok := obj["error"].(map[string]any)
	if !ok {
		return nil
	}
	rawMessage := firstPresent(rawError, "message", "error")
	if rawMessage == "" {
		rawMessage = "Upstream stream error"
	}
	message := fmt.Sprint(rawMessage)
	text := strings.ToLower(message)
	status := 502
	if numericCode(rawError["code"]) == 8 || strings.Contains(text, "too many requests") || strings.Contains(text, "rate limit") {
		status = 429
	}
	return platform.NewUpstreamError("Upstream stream error: "+message, status, marshalStreamErrorBody(rawError))
}

func defaultDeviceEnvInfo() map[string]any {
	return map[string]any{
		"darkModeEnabled":  false,
		"devicePixelRatio": 2,
		"screenHeight":     1329,
		"screenWidth":      2056,
		"viewportHeight":   1083,
		"viewportWidth":    2056,
	}
}

func firstPresent(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func numericCode(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := strconv.Atoi(v.String())
		return i
	default:
		return 0
	}
}

func marshalStreamErrorBody(errorObj map[string]any) string {
	if message, ok := errorObj["message"]; ok {
		body := fmt.Sprintf(`{"error":{"message":%s`, mustJSON(message))
		if code, ok := errorObj["code"]; ok {
			body += fmt.Sprintf(`,"code":%s`, mustJSON(code))
		}
		return truncateStreamErrorBody(body + "}}")
	}
	if errText, ok := errorObj["error"]; ok {
		return truncateStreamErrorBody(fmt.Sprintf(`{"error":{"error":%s}}`, mustJSON(errText)))
	}
	body, err := json.Marshal(map[string]any{"error": errorObj})
	if err != nil {
		return fmt.Sprint(map[string]any{"error": errorObj})
	}
	return truncateStreamErrorBody(string(body))
}

func mustJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return strconv.Quote(fmt.Sprint(value))
	}
	return string(body)
}

func truncateStreamErrorBody(body string) string {
	if len(body) > 400 {
		return body[:400]
	}
	return body
}
