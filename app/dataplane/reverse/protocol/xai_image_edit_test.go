package protocol

import (
	"reflect"
	"testing"
)

func TestImageEditPayloadMatchesPythonShape(t *testing.T) {
	refs := []string{"asset-1", "asset-2"}
	payload := BuildImageEditPayload(ImageEditPayloadOptions{
		Prompt:          "make it brighter",
		ImageReferences: refs,
		ParentPostID:    "post-1",
	})

	if ImageEditModelName != "imagine-image-edit" || ImageEditModelKind != "imagine" ||
		ImagePostMediaType != "MEDIA_POST_TYPE_IMAGE" || ImageEditGenerationCount != 2 {
		t.Fatalf("image edit constants mismatch")
	}
	wantTop := map[string]any{
		"temporary":                 true,
		"modelName":                 "imagine-image-edit",
		"message":                   "make it brighter",
		"enableImageGeneration":     true,
		"returnImageBytes":          false,
		"enableImageStreaming":      true,
		"imageGenerationCount":      2,
		"disableTextFollowUps":      true,
		"disableMemory":             true,
		"forceSideBySide":           false,
		"returnRawGrokInXaiRequest": false,
	}
	for key, want := range wantTop {
		if !reflect.DeepEqual(want, payload[key]) {
			t.Fatalf("payload[%s] mismatch want=%#v got=%#v", key, want, payload[key])
		}
	}
	modelMap := payload["responseMetadata"].(map[string]any)["modelConfigOverride"].(map[string]any)["modelMap"].(map[string]any)
	if modelMap["imageEditModel"] != "imagine" {
		t.Fatalf("imageEditModel mismatch: %#v", modelMap)
	}
	config := modelMap["imageEditModelConfig"].(map[string]any)
	if !reflect.DeepEqual([]string{"asset-1", "asset-2"}, config["imageReferences"]) || config["parentPostId"] != "post-1" {
		t.Fatalf("image edit config mismatch: %#v", config)
	}
	refs[0] = "asset-mutated"
	if !reflect.DeepEqual([]string{"asset-mutated", "asset-2"}, config["imageReferences"]) {
		t.Fatalf("Python payload keeps image_references list identity, got %#v", config["imageReferences"])
	}

	temporary := false
	payload = BuildImageEditPayload(ImageEditPayloadOptions{
		Prompt:        "",
		ParentPostID:  "",
		Temporary:     &temporary,
		MemoryEnabled: true,
	})
	if payload["temporary"] != false || payload["disableMemory"] != false || payload["message"] != "" {
		t.Fatalf("temporary/memory empty prompt mismatch: %#v", payload)
	}
	modelMap = payload["responseMetadata"].(map[string]any)["modelConfigOverride"].(map[string]any)["modelMap"].(map[string]any)
	config = modelMap["imageEditModelConfig"].(map[string]any)
	if !reflect.DeepEqual([]string(nil), config["imageReferences"]) || config["parentPostId"] != "" {
		t.Fatalf("empty image edit config mismatch: %#v", config)
	}
}

func TestImageEditExtractorsMatchPythonFiltering(t *testing.T) {
	data := map[string]any{"result": map[string]any{"response": map[string]any{
		"streamingImageGenerationResponse": map[string]any{"progress": float64(50)},
		"modelResponse": map[string]any{
			"generatedImageUrls": []any{"https://a", "", 7, "https://b"},
			"fileAttachments":    []any{"asset-1", "", nil, "asset-2"},
		},
	}}}
	if !reflect.DeepEqual(map[string]any{"progress": float64(50)}, ExtractStreamingImageEditResponse(data)) {
		t.Fatalf("stream response mismatch")
	}
	if !reflect.DeepEqual([]string{"https://a", "https://b"}, ExtractModelResponseImageEditURLs(data)) {
		t.Fatalf("urls mismatch")
	}
	if !reflect.DeepEqual([]string{"asset-1", "asset-2"}, ExtractModelResponseImageEditFileAttachments(data)) {
		t.Fatalf("attachments mismatch")
	}
	if ExtractStreamingImageEditResponse(map[string]any{}) != nil {
		t.Fatalf("missing stream should return nil")
	}
	if ExtractStreamingImageEditResponse(map[string]any{"result": "bad"}) != nil {
		t.Fatalf("non-object result should return nil stream")
	}
	malformed := map[string]any{"result": map[string]any{"response": map[string]any{"modelResponse": map[string]any{
		"generatedImageUrls": "bad",
		"fileAttachments":    []any{false, "", 0},
	}}}}
	if !reflect.DeepEqual([]string{}, ExtractModelResponseImageEditURLs(malformed)) {
		t.Fatalf("non-list generatedImageUrls should return empty")
	}
	if !reflect.DeepEqual([]string{}, ExtractModelResponseImageEditFileAttachments(malformed)) {
		t.Fatalf("non-string fileAttachments should return empty")
	}
}
