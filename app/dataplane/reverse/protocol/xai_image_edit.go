package protocol

const (
	ImageEditModelName       = "imagine-image-edit"
	ImageEditModelKind       = "imagine"
	ImagePostMediaType       = "MEDIA_POST_TYPE_IMAGE"
	ImageEditGenerationCount = 2
)

type ImageEditPayloadOptions struct {
	Prompt          string
	ImageReferences []string
	ParentPostID    string
	Temporary       *bool
	MemoryEnabled   bool
}

func BuildImageEditPayload(options ImageEditPayloadOptions) map[string]any {
	temporary := true
	if options.Temporary != nil {
		temporary = *options.Temporary
	}
	return map[string]any{
		"temporary":                 temporary,
		"modelName":                 ImageEditModelName,
		"message":                   options.Prompt,
		"enableImageGeneration":     true,
		"returnImageBytes":          false,
		"returnRawGrokInXaiRequest": false,
		"enableImageStreaming":      true,
		"imageGenerationCount":      ImageEditGenerationCount,
		"forceConcise":              false,
		"enableSideBySide":          true,
		"sendFinalMetadata":         true,
		"isReasoning":               false,
		"disableTextFollowUps":      true,
		"responseMetadata": map[string]any{
			"modelConfigOverride": map[string]any{
				"modelMap": map[string]any{
					"imageEditModel": ImageEditModelKind,
					"imageEditModelConfig": map[string]any{
						"imageReferences": options.ImageReferences,
						"parentPostId":    options.ParentPostID,
					},
				},
			},
		},
		"disableMemory":   !options.MemoryEnabled,
		"forceSideBySide": false,
	}
}

func ExtractStreamingImageEditResponse(data map[string]any) map[string]any {
	response := nestedMap(data, "result", "response")
	if response == nil {
		return nil
	}
	stream, _ := response["streamingImageGenerationResponse"].(map[string]any)
	return stream
}

func ExtractModelResponseImageEditURLs(data map[string]any) []string {
	modelResponse := nestedMap(data, "result", "response", "modelResponse")
	if modelResponse == nil {
		return []string{}
	}
	return nonEmptyStringItems(modelResponse["generatedImageUrls"])
}

func ExtractModelResponseImageEditFileAttachments(data map[string]any) []string {
	modelResponse := nestedMap(data, "result", "response", "modelResponse")
	if modelResponse == nil {
		return []string{}
	}
	return nonEmptyStringItems(modelResponse["fileAttachments"])
}

func nestedMap(data map[string]any, keys ...string) map[string]any {
	current := data
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func nonEmptyStringItems(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, item := range items {
		text, ok := item.(string)
		if ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}
