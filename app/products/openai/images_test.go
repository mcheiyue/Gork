package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	"github.com/jiujiu532/grok2api/app/platform"
)

func TestImagesResolveAspectRatioAndResponseFormat(t *testing.T) {
	resetImagesDepsForTest(t)

	if got := ResolveAspectRatio("1280x720"); got != "16:9" {
		t.Fatalf("1280x720 aspect=%q", got)
	}
	if got := ResolveAspectRatio("not-a-size"); got != "2:3" {
		t.Fatalf("fallback aspect=%q", got)
	}
	if got, err := normalizeImageResponseFormat(" B64_JSON "); err != nil || got != "b64_json" {
		t.Fatalf("format=%q err=%v", got, err)
	}
	var validation *platform.ValidationError
	if _, err := normalizeImageResponseFormat("gif"); !errors.As(err, &validation) || validation.Param != "response_format" {
		t.Fatalf("validation err=%#v", err)
	}
}

func TestImagesProgressHelpersMatchPythonBehavior(t *testing.T) {
	if clampProgress(-5) != 0 || clampProgress(120) != 100 || clampProgress(42) != 42 {
		t.Fatalf("clamp mismatch")
	}
	progress := computeProgressPercent(map[any]int{"a": 50, "b": 100, "c": 20}, 2)
	if progress != 75 {
		t.Fatalf("progress=%d", progress)
	}
	if completedItems(map[any]int{"a": 99, "b": 100, "c": 120}) != 2 {
		t.Fatalf("completed mismatch")
	}
	if got := progressReason("图片", 75, 1, 2); got != "正在生成 图片75% (1/2)" {
		t.Fatalf("reason=%q", got)
	}
}

func TestImagesResolveOutputUsesPublicURLWhenProxyDisabled(t *testing.T) {
	resetImagesDepsForTest(t)
	imageAppURL = func() string { return "https://local.test" }
	imagePublicProxyEnabled = func() bool { return false }
	imageDownloadBytes = func(context.Context, string, string) ([]byte, string, error) {
		t.Fatalf("public image should not be downloaded")
		return nil, "", nil
	}

	output, err := resolveImageOutput(context.Background(), imageOutputOptions{
		Token:          "tok",
		URL:            "https://imagine-public.grok.com/generated/image.png",
		ResponseFormat: "url",
	})
	if err != nil {
		t.Fatalf("resolveImageOutput err=%v", err)
	}
	if output.APIValue != "https://imagine-public.grok.com/generated/image.png" {
		t.Fatalf("output=%#v", output)
	}
	if outputContent(output, true) != "![image](https://imagine-public.grok.com/generated/image.png)" {
		t.Fatalf("markdown=%q", outputContent(output, true))
	}
}

func TestImagesResolveOutputSavesLocalURLOrReturnsBase64(t *testing.T) {
	resetImagesDepsForTest(t)
	imageAppURL = func() string { return "https://api.local" }
	imagePublicProxyEnabled = func() bool { return true }
	imageDownloadBytes = func(_ context.Context, token, rawURL string) ([]byte, string, error) {
		if token != "tok" || !strings.Contains(rawURL, "abc123.png") {
			t.Fatalf("download token/url=%q/%q", token, rawURL)
		}
		return []byte("image-bytes"), "image/png", nil
	}
	saved := map[string]string{}
	imageSaveLocal = func(raw []byte, mime, fileID string) (string, error) {
		saved["raw"] = string(raw)
		saved["mime"] = mime
		saved["fileID"] = fileID
		return fileID + "-saved", nil
	}

	local, err := resolveImageOutput(context.Background(), imageOutputOptions{
		Token:          "tok",
		URL:            "https://imagine-public.grok.com/images/abc123.png",
		ResponseFormat: "url",
	})
	if err != nil {
		t.Fatalf("resolve local err=%v", err)
	}
	if local.APIValue != "https://api.local/v1/files/image?id=abc123-saved" {
		t.Fatalf("local=%#v saved=%#v", local, saved)
	}
	if !reflect.DeepEqual(saved, map[string]string{"raw": "image-bytes", "mime": "image/png", "fileID": "abc123"}) {
		t.Fatalf("saved=%#v", saved)
	}

	blob := base64.StdEncoding.EncodeToString([]byte("blob-data"))
	b64, err := resolveImageOutput(context.Background(), imageOutputOptions{
		Token:          "tok",
		URL:            "https://assets.local/final.jpg",
		ResponseFormat: "b64_json",
		BlobB64:        blob,
	})
	if err != nil {
		t.Fatalf("resolve b64 err=%v", err)
	}
	if b64.APIValue != blob || !strings.Contains(b64.MarkdownValue, "data:image/jpeg;base64,"+blob) {
		t.Fatalf("b64=%#v", b64)
	}
}

func TestImagesEditHelpersMatchPythonBehavior(t *testing.T) {
	resetImagesDepsForTest(t)
	messages := []map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": " https://img/1.png "}},
			map[string]any{"type": "text", "text": " first prompt "},
		}},
		{"role": "user", "content": " final prompt "},
		{"role": "user", "content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img/2.png"}},
		}},
	}

	prompt, inputs, err := extractEditPromptAndInputs(messages)
	if err != nil {
		t.Fatalf("extract prompt err=%v", err)
	}
	if prompt != "final prompt" || !reflect.DeepEqual(inputs, []string{"https://img/1.png", "https://img/2.png"}) {
		t.Fatalf("prompt/inputs=%q/%#v", prompt, inputs)
	}

	many := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8"}
	if got, err := normalizeEditInputs(many); err != nil || !reflect.DeepEqual(got, []string{"2", "3", "4", "5", "6", "7", "8"}) {
		t.Fatalf("normalize inputs=%#v err=%v", got, err)
	}
	if got, err := normalizeEditSize("1024x1024"); err != nil || got != "1024x1024" {
		t.Fatalf("size=%q err=%v", got, err)
	}
	var validation *platform.ValidationError
	if _, err := normalizeEditSize("512x512"); !errors.As(err, &validation) || validation.Param != "size" {
		t.Fatalf("size validation=%#v", err)
	}

	replaced := replaceEditImagePlaceholders("blend @IMAGE1 with @image2 and keep @IMAGE3", []editReference{
		{FileID: "file_a"},
		{FileID: "file_b"},
	})
	if replaced != "blend @file_a with @file_b and keep @IMAGE3" {
		t.Fatalf("replaced=%q", replaced)
	}
	if got := parseImageIndex("3"); got == nil || *got != 3 {
		t.Fatalf("index=%v", got)
	}
	if got := parseImageIndex("-1"); got != nil {
		t.Fatalf("negative index=%v", *got)
	}
}

func TestImagesGenerateNonStreamUsesImagineEvents(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-img", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	imageAppURL = func() string { return "https://api.local" }
	imagePublicProxyEnabled = func() bool { return true }
	imageNowUnix = func() int64 { return 1234 }
	imageResponseID = func() string { return "chatcmpl-image" }
	imageDownloadBytes = func(_ context.Context, token, rawURL string) ([]byte, string, error) {
		return []byte("raw:" + token + ":" + rawURL), "image/png", nil
	}
	imageSaveLocal = func(raw []byte, mime, fileID string) (string, error) {
		return fileID + "-" + mime + "-" + string(raw), nil
	}
	imageStreamImages = func(_ context.Context, token, prompt string, options transport.ImagineOptions) ([]map[string]any, error) {
		if token != "tok-img" || prompt != "draw" || options.AspectRatio != "16:9" || options.N != 2 || options.EnablePro {
			t.Fatalf("stream args token=%q prompt=%q options=%#v", token, prompt, options)
		}
		if options.EnableNSFW == nil || !*options.EnableNSFW {
			t.Fatalf("EnableNSFW=%v", options.EnableNSFW)
		}
		return []map[string]any{
			{"type": "progress", "image_id": "img1", "progress": 50},
			{"type": "completed", "image_id": "img1", "url": "https://imagine-public.grok.com/images/one.png", "is_final": true},
			{"type": "completed", "image_id": "img2", "url": "https://imagine-public.grok.com/images/two.png", "is_final": true},
		}, nil
	}

	result, err := GenerateImages(context.Background(), imageGenerationOptions{
		Model:          "grok-imagine-image",
		Prompt:         "draw",
		N:              2,
		Size:           "1280x720",
		ResponseFormat: "url",
	})
	if err != nil {
		t.Fatalf("GenerateImages err=%v", err)
	}
	if result.IsStream {
		t.Fatalf("expected non-stream result: %#v", result)
	}
	if result.Response["created"] != int64(1234) {
		t.Fatalf("created=%#v", result.Response["created"])
	}
	data := result.Response["data"].([]map[string]any)
	if len(data) != 2 || !strings.Contains(data[0]["url"].(string), "one-image/png-raw:tok-img") || !strings.Contains(data[1]["url"].(string), "two-image/png-raw:tok-img") {
		t.Fatalf("data=%#v", data)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 0 {
		t.Fatalf("dir releases=%d feedbacks=%#v", dir.releases, dir.feedbacks)
	}
}

func TestImagesGenerateStreamReturnsFrames(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-img", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	imageResponseID = func() string { return "chatcmpl-image-stream" }
	imageStreamImages = func(context.Context, string, string, transport.ImagineOptions) ([]map[string]any, error) {
		return []map[string]any{
			{"type": "completed", "image_id": "img1", "url": "https://imagine-public.grok.com/images/one.png", "is_final": true},
		}, nil
	}

	result, err := GenerateImages(context.Background(), imageGenerationOptions{
		Model:  "grok-imagine-image",
		Prompt: "draw",
		Stream: true,
	})
	if err != nil {
		t.Fatalf("GenerateImages stream err=%v", err)
	}
	if !result.IsStream {
		t.Fatalf("expected stream result: %#v", result)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, "https://imagine-public.grok.com/images/one.png") || !strings.Contains(joined, "data: [DONE]") {
		t.Fatalf("frames=%s", joined)
	}
}

func TestImagesGenerateStreamChatFormatEmitsProgressThinking(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-img", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	imageResponseID = func() string { return "chatcmpl-image-progress" }
	imageStreamImages = func(context.Context, string, string, transport.ImagineOptions) ([]map[string]any, error) {
		return []map[string]any{
			{"type": "progress", "image_id": "img1", "progress": 50},
			{"type": "completed", "image_id": "img1", "url": "https://imagine-public.grok.com/images/one.png", "is_final": true},
		}, nil
	}

	result, err := GenerateImages(context.Background(), imageGenerationOptions{
		Model:      "grok-imagine-image",
		Prompt:     "draw",
		Stream:     true,
		ChatFormat: true,
	})
	if err != nil {
		t.Fatalf("GenerateImages stream chat err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, "正在生成 图片50%") || !strings.Contains(joined, "![image](https://imagine-public.grok.com/images/one.png)") {
		t.Fatalf("frames=%s", joined)
	}
}

func TestImagesGenerateNonStreamChatFormatIncludesProgressReasoning(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-img", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	imageResponseID = func() string { return "chatcmpl-image-reasoning" }
	imageStreamImages = func(context.Context, string, string, transport.ImagineOptions) ([]map[string]any, error) {
		return []map[string]any{
			{"type": "progress", "image_id": "img1", "progress": 50},
			{"type": "completed", "image_id": "img1", "url": "https://imagine-public.grok.com/images/one.png", "is_final": true},
		}, nil
	}

	result, err := GenerateImages(context.Background(), imageGenerationOptions{
		Model:      "grok-imagine-image",
		Prompt:     "draw",
		ChatFormat: true,
	})
	if err != nil {
		t.Fatalf("GenerateImages chat err=%v", err)
	}
	choices := result.Response["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	reasoning, _ := message["reasoning_content"].(string)
	if !strings.Contains(reasoning, "正在生成 图片50%") {
		t.Fatalf("message=%#v", message)
	}
}

func TestImagesGenerateLiteUsesLiteBatchAndFormatsChat(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	imageResponseID = func() string { return "chatcmpl-lite" }
	called := false
	imageRunLiteBatch = func(_ context.Context, options imageLiteBatchOptions) ([]imageOutput, error) {
		called = true
		if options.Model != "grok-imagine-image-lite" || options.Prompt != "draw lite" || options.N != 2 || options.ResponseFormat != "url" {
			t.Fatalf("lite options=%#v", options)
		}
		if options.ProgressCB == nil {
			t.Fatalf("lite progress callback missing")
		}
		options.ProgressCB(0, 50)
		options.ProgressCB(0, 100)
		return []imageOutput{
			{APIValue: "https://lite/1.png", MarkdownValue: "![image](https://lite/1.png)"},
			{APIValue: "https://lite/2.png", MarkdownValue: "![image](https://lite/2.png)"},
		}, nil
	}

	result, err := GenerateImages(context.Background(), imageGenerationOptions{
		Model:          "grok-imagine-image-lite",
		Prompt:         "draw lite",
		N:              2,
		ResponseFormat: "url",
		ChatFormat:     true,
	})
	if err != nil {
		t.Fatalf("GenerateImages lite err=%v", err)
	}
	if !called {
		t.Fatalf("lite batch was not called")
	}
	choices := result.Response["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if !strings.Contains(message["content"].(string), "https://lite/1.png") || !strings.Contains(message["content"].(string), "https://lite/2.png") {
		t.Fatalf("message=%#v", message)
	}
	if !strings.Contains(message["reasoning_content"].(string), "正在生成 图片50%") {
		t.Fatalf("message=%#v", message)
	}
}

func TestImagesGenerateLiteStreamChatFormatEmitsProgressThinking(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-lite", ModeID: model.ModeFast}}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	imageResponseID = func() string { return "chatcmpl-lite-stream" }
	imageRunLiteBatch = runLiteBatch
	imageStreamLiteGenerate = func(_ context.Context, token, prompt string, modeID model.ModeID) ([]string, error) {
		if token != "tok-lite" || prompt != "draw lite" || modeID != model.ModeFast {
			t.Fatalf("lite stream token/prompt/mode=%q/%q/%v", token, prompt, modeID)
		}
		return []string{
			`data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img-card\",\"image_chunk\":{\"progress\":50,\"imageUuid\":\"uuid1\",\"moderated\":false}}"}}}}`,
			`data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img-card\",\"image_chunk\":{\"progress\":100,\"imageUuid\":\"uuid1\",\"imageUrl\":\"generated/lite.png\",\"moderated\":false}}"}}}}`,
			`data: [DONE]`,
		}, nil
	}

	result, err := GenerateImages(context.Background(), imageGenerationOptions{
		Model:      "grok-imagine-image-lite",
		Prompt:     "draw lite",
		Stream:     true,
		ChatFormat: true,
	})
	if err != nil {
		t.Fatalf("GenerateImages lite stream chat err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, "正在生成 图片50%") || !strings.Contains(joined, "![image](https://assets.grok.com/generated/lite.png)") {
		t.Fatalf("frames=%s", joined)
	}
}

func TestImagesRunLiteBatchRetriesAndParsesImageEvent(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{
		{Token: "tokA", ModeID: model.ModeFast},
		{Token: "tokB", ModeID: model.ModeFast},
	}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	chatSelectionMaxRetries = func() int { return 1 }
	chatRetryConfig = func() map[string]any { return map[string]any{"retry.on_codes": "429"} }
	imageStreamLiteGenerate = func(_ context.Context, token, prompt string, modeID model.ModeID) ([]string, error) {
		if prompt != "draw lite" || modeID != model.ModeFast {
			t.Fatalf("lite stream prompt/mode=%q/%v", prompt, modeID)
		}
		if token == "tokA" {
			return nil, platform.NewUpstreamError("rate limited", 429, "")
		}
		return []string{
			`data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img-card\",\"image_chunk\":{\"progress\":100,\"imageUuid\":\"uuid1\",\"imageUrl\":\"generated/foo.png\",\"moderated\":false}}"}}}}`,
			`data: [DONE]`,
		}, nil
	}

	images, err := runLiteBatch(context.Background(), imageLiteBatchOptions{
		Model:          "grok-imagine-image-lite",
		Prompt:         "draw lite",
		N:              1,
		ResponseFormat: "url",
	})
	if err != nil {
		t.Fatalf("runLiteBatch err=%v", err)
	}
	if len(images) != 1 || images[0].APIValue != "https://assets.grok.com/generated/foo.png" {
		t.Fatalf("images=%#v", images)
	}
	if !reflect.DeepEqual(dir.excludes, [][]string{{}, {"tokA"}}) {
		t.Fatalf("excludes=%#v", dir.excludes)
	}
	if len(dir.feedbacks) != 2 || dir.feedbacks[0].Kind != feedbackKindRateLimited || dir.feedbacks[1].Kind != feedbackKindSuccess {
		t.Fatalf("feedbacks=%#v", dir.feedbacks)
	}
	if refresh.refreshCalls != 1 || refresh.recordCalls != 1 || refresh.onDemandCalls != 1 {
		t.Fatalf("refresh=%#v", refresh)
	}
}

func TestImagesEditNonStreamUploadsCreatesPostAndCollectsImages(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-edit", ModeID: model.ModeAuto}}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	imageNowUnix = func() int64 { return 2222 }
	imageResponseID = func() string { return "chatcmpl-edit" }
	uploads := []string{}
	imageUploadFromInput = func(_ context.Context, token, input string) (transport.AssetUploadResult, error) {
		if token != "tok-edit" {
			t.Fatalf("upload token=%q", token)
		}
		uploads = append(uploads, input)
		return transport.AssetUploadResult{FileID: "file_" + input[len(input)-5:len(input)-4], FileURI: "uri_" + input[len(input)-5:len(input)-4]}, nil
	}
	imageResolveUploadedAssetReference = func(token, fileID, fileURI string) (string, error) {
		return "https://asset.local/" + token + "/" + fileID + "/" + fileURI, nil
	}
	imageCreateMediaPost = func(_ context.Context, token, mediaType string, options transport.MediaOptions) (map[string]any, error) {
		if token != "tok-edit" || mediaType != "MEDIA_POST_TYPE_IMAGE" || options.Prompt != "blend @file_1 with @file_2" {
			t.Fatalf("create media token/type/options=%q/%q/%#v", token, mediaType, options)
		}
		return map[string]any{"post": map[string]any{"id": "post_123", "originalPrompt": "server prompt"}}, nil
	}
	imageCollectEditImages = func(_ context.Context, options imageCollectEditOptions) ([]imageOutput, error) {
		if options.Token != "tok-edit" || options.Prompt != "server prompt" || options.ParentPostID != "post_123" || options.RequestedN != 2 {
			t.Fatalf("collect options=%#v", options)
		}
		wantRefs := []string{
			"https://asset.local/tok-edit/file_1/uri_1",
			"https://asset.local/tok-edit/file_2/uri_2",
		}
		if !reflect.DeepEqual(options.ImageReferences, wantRefs) {
			t.Fatalf("refs=%#v", options.ImageReferences)
		}
		return []imageOutput{{APIValue: "https://out/1.png"}, {APIValue: "https://out/2.png"}}, nil
	}

	result, err := EditImages(context.Background(), imageEditOptions{
		Model: "grok-imagine-image-edit",
		Messages: []map[string]any{{"role": "user", "content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img/1.png"}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img/2.png"}},
			map[string]any{"type": "text", "text": "blend @IMAGE1 with @IMAGE2"},
		}}},
		N:              2,
		Size:           "1024x1024",
		ResponseFormat: "url",
	})
	if err != nil {
		t.Fatalf("EditImages err=%v", err)
	}
	if !reflect.DeepEqual(uploads, []string{"https://img/1.png", "https://img/2.png"}) {
		t.Fatalf("uploads=%#v", uploads)
	}
	if result.Response["created"] != int64(2222) {
		t.Fatalf("created=%#v", result.Response["created"])
	}
	data := result.Response["data"].([]map[string]any)
	if !reflect.DeepEqual(data, []map[string]any{{"url": "https://out/1.png"}, {"url": "https://out/2.png"}}) {
		t.Fatalf("data=%#v", data)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 1 || dir.feedbacks[0].Kind != feedbackKindSuccess {
		t.Fatalf("dir releases=%d feedbacks=%#v", dir.releases, dir.feedbacks)
	}
	if refresh.refreshCalls != 1 || refresh.token != "tok-edit" {
		t.Fatalf("refresh=%#v", refresh)
	}
}

func TestImagesEditStreamReturnsFrames(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-edit", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	imageResponseID = func() string { return "chatcmpl-edit-stream" }
	imageUploadFromInput = func(context.Context, string, string) (transport.AssetUploadResult, error) {
		return transport.AssetUploadResult{FileID: "file_1", FileURI: "uri_1"}, nil
	}
	imageResolveUploadedAssetReference = func(string, string, string) (string, error) {
		return "https://asset/ref.png", nil
	}
	imageCreateMediaPost = func(context.Context, string, string, transport.MediaOptions) (map[string]any, error) {
		return map[string]any{"post": map[string]any{"id": "post_1"}}, nil
	}
	imageCollectEditImages = func(context.Context, imageCollectEditOptions) ([]imageOutput, error) {
		return []imageOutput{{APIValue: "https://out/edit.png", MarkdownValue: "![image](https://out/edit.png)"}}, nil
	}

	result, err := EditImages(context.Background(), imageEditOptions{
		Model: "grok-imagine-image-edit",
		Messages: []map[string]any{{"content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img/1.png"}},
			map[string]any{"type": "text", "text": "edit it"},
		}}},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("EditImages stream err=%v", err)
	}
	if !result.IsStream {
		t.Fatalf("expected stream result: %#v", result)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, "https://out/edit.png") || !strings.Contains(joined, "data: [DONE]") {
		t.Fatalf("frames=%s", joined)
	}
}

func TestImagesEditStreamChatFormatEmitsProgressThinking(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-edit", ModeID: model.ModeAuto}}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	imageResponseID = func() string { return "chatcmpl-edit-progress" }
	imageCollectEditImages = collectEditImages
	imageUploadFromInput = func(context.Context, string, string) (transport.AssetUploadResult, error) {
		return transport.AssetUploadResult{FileID: "file_1", FileURI: "uri_1"}, nil
	}
	imageResolveUploadedAssetReference = func(string, string, string) (string, error) {
		return "https://asset/ref.png", nil
	}
	imageCreateMediaPost = func(context.Context, string, string, transport.MediaOptions) (map[string]any, error) {
		return map[string]any{"post": map[string]any{"id": "post_1"}}, nil
	}
	imageStreamEditLines = func(context.Context, imageCollectEditOptions) ([]string, error) {
		return []string{
			`data: {"result":{"response":{"streamingImageGenerationResponse":{"imageIndex":0,"progress":50,"moderated":false}}}}`,
			`data: {"result":{"response":{"streamingImageGenerationResponse":{"imageIndex":0,"progress":100,"imageUrl":"https://edit/one.png","moderated":false}}}}`,
			`data: [DONE]`,
		}, nil
	}

	result, err := EditImages(context.Background(), imageEditOptions{
		Model: "grok-imagine-image-edit",
		Messages: []map[string]any{{"content": []any{
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img/1.png"}},
			map[string]any{"type": "text", "text": "edit it"},
		}}},
		Stream:     true,
		ChatFormat: true,
	})
	if err != nil {
		t.Fatalf("EditImages stream chat err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, "正在生成 图片50%") || !strings.Contains(joined, "![image](https://edit/one.png)") {
		t.Fatalf("frames=%s", joined)
	}
}

func TestImagesCollectEditImagesFromStreamingPayloads(t *testing.T) {
	resetImagesDepsForTest(t)
	imageAppURL = func() string { return "" }
	calls := 0
	imageStreamEditLines = func(_ context.Context, options imageCollectEditOptions) ([]string, error) {
		calls++
		if options.Token != "tok" || options.Prompt != "edit prompt" || options.ParentPostID != "post" {
			t.Fatalf("stream options=%#v", options)
		}
		return []string{
			`data: {"result":{"response":{"streamingImageGenerationResponse":{"imageIndex":0,"progress":100,"imageUrl":"https://edit/1.png","moderated":false}}}}`,
			`data: {"result":{"response":{"modelResponse":{"generatedImageUrls":["https://edit/1-duplicate.png","https://edit/2.png"]}}}}`,
			`data: [DONE]`,
		}, nil
	}

	images, err := collectEditImages(context.Background(), imageCollectEditOptions{
		Token:           "tok",
		Prompt:          "edit prompt",
		ImageReferences: []string{"https://asset/ref.png"},
		ParentPostID:    "post",
		RequestedN:      2,
		ResponseFormat:  "url",
	})
	if err != nil {
		t.Fatalf("collectEditImages err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("stream calls=%d", calls)
	}
	if !reflect.DeepEqual(images, []imageOutput{
		{APIValue: "https://edit/1.png", MarkdownValue: "![image](https://edit/1.png)"},
		{APIValue: "https://edit/2.png", MarkdownValue: "![image](https://edit/2.png)"},
	}) {
		t.Fatalf("images=%#v", images)
	}
}

func TestImagesCollectEditFinalURLsResolvesAssetIDsWithUserID(t *testing.T) {
	resetImagesDepsForTest(t)
	imageStreamEditLines = func(_ context.Context, options imageCollectEditOptions) ([]string, error) {
		if options.Token != "sso=abc; x-userid=user-1" {
			t.Fatalf("token=%q", options.Token)
		}
		return []string{
			`data: {"result":{"response":{"streamingImageGenerationResponse":{"imageIndex":1,"progress":100,"assetId":"file-stream","moderated":false}}}}`,
			`data: {"result":{"response":{"modelResponse":{"fileAttachments":["file-meta"]}}}}`,
			`data: [DONE]`,
		}, nil
	}

	finalURLs, err := collectEditFinalURLs(context.Background(), imageCollectEditOptions{
		Token:           "sso=abc; x-userid=user-1",
		Prompt:          "edit prompt",
		ImageReferences: []string{"https://asset/ref.png"},
		ParentPostID:    "post",
	})
	if err != nil {
		t.Fatalf("collectEditFinalURLs err=%v", err)
	}
	want := map[int]string{
		0: "https://assets.grok.com/users/user-1/file-meta/content",
		1: "https://assets.grok.com/users/user-1/file-stream/content",
	}
	if !reflect.DeepEqual(finalURLs, want) {
		t.Fatalf("finalURLs=%#v want %#v", finalURLs, want)
	}
}

func TestImagesStreamImageEditLinesPostsPayload(t *testing.T) {
	resetChatDepsForTest(t)
	resetImagesDepsForTest(t)
	streamPost = func(_ context.Context, req chatStreamRequest) (*chatStreamResponse, error) {
		if req.Token != "tok" || req.Headers["referer"] != "https://grok.com/imagine/post/post_1" {
			t.Fatalf("request=%#v", req)
		}
		body := string(req.PayloadBytes)
		if !strings.Contains(body, `"message":"edit prompt"`) || !strings.Contains(body, `"parentPostId":"post_1"`) {
			t.Fatalf("payload=%s", body)
		}
		return &chatStreamResponse{StatusCode: 200, Lines: []string{"data: {}", "data: [DONE]"}}, nil
	}

	lines, err := streamImageEditLines(context.Background(), imageCollectEditOptions{
		Token:           "tok",
		Prompt:          "edit prompt",
		ImageReferences: []string{"https://asset/ref.png"},
		ParentPostID:    "post_1",
	})
	if err != nil {
		t.Fatalf("streamImageEditLines err=%v", err)
	}
	if !reflect.DeepEqual(lines, []string{"data: {}", "data: [DONE]"}) {
		t.Fatalf("lines=%#v", lines)
	}
}

func resetImagesDepsForTest(t *testing.T) {
	t.Helper()
	oldAppURL := imageAppURL
	oldProxy := imagePublicProxyEnabled
	oldDownload := imageDownloadBytes
	oldSave := imageSaveLocal
	oldStreamImages := imageStreamImages
	oldUploadFromInput := imageUploadFromInput
	oldResolveUploaded := imageResolveUploadedAssetReference
	oldCreateMediaPost := imageCreateMediaPost
	oldCollectEditImages := imageCollectEditImages
	oldStreamEditLines := imageStreamEditLines
	oldRunLiteBatch := imageRunLiteBatch
	oldStreamLite := imageStreamLiteGenerate
	oldNowUnix := imageNowUnix
	oldResponseID := imageResponseID

	imageAppURL = func() string { return "" }
	imagePublicProxyEnabled = func() bool { return false }
	imageDownloadBytes = func(context.Context, string, string) ([]byte, string, error) {
		return []byte("image"), "image/jpeg", nil
	}
	imageSaveLocal = func(_ []byte, _ string, fileID string) (string, error) {
		return fileID, nil
	}
	imageStreamImages = func(context.Context, string, string, transport.ImagineOptions) ([]map[string]any, error) {
		return nil, platform.NewUpstreamError("image stream transport is not configured", 502, "")
	}
	imageUploadFromInput = func(context.Context, string, string) (transport.AssetUploadResult, error) {
		return transport.AssetUploadResult{}, platform.NewUpstreamError("asset upload is not configured", 502, "")
	}
	imageResolveUploadedAssetReference = func(string, string, string) (string, error) {
		return "", platform.NewUpstreamError("asset resolve is not configured", 502, "")
	}
	imageCreateMediaPost = func(context.Context, string, string, transport.MediaOptions) (map[string]any, error) {
		return nil, platform.NewUpstreamError("media post is not configured", 502, "")
	}
	imageCollectEditImages = func(context.Context, imageCollectEditOptions) ([]imageOutput, error) {
		return nil, platform.NewUpstreamError("image edit collection is not configured", 502, "")
	}
	imageStreamEditLines = func(context.Context, imageCollectEditOptions) ([]string, error) {
		return nil, platform.NewUpstreamError("image edit stream is not configured", 502, "")
	}
	imageRunLiteBatch = func(context.Context, imageLiteBatchOptions) ([]imageOutput, error) {
		return nil, platform.NewUpstreamError("lite image generation is not configured", 502, "")
	}
	imageStreamLiteGenerate = func(context.Context, string, string, model.ModeID) ([]string, error) {
		return nil, platform.NewUpstreamError("lite image stream is not configured", 502, "")
	}
	imageNowUnix = func() int64 { return 1 }
	imageResponseID = func() string { return "chatcmpl-test" }

	t.Cleanup(func() {
		imageAppURL = oldAppURL
		imagePublicProxyEnabled = oldProxy
		imageDownloadBytes = oldDownload
		imageSaveLocal = oldSave
		imageStreamImages = oldStreamImages
		imageUploadFromInput = oldUploadFromInput
		imageResolveUploadedAssetReference = oldResolveUploaded
		imageCreateMediaPost = oldCreateMediaPost
		imageCollectEditImages = oldCollectEditImages
		imageStreamEditLines = oldStreamEditLines
		imageRunLiteBatch = oldRunLiteBatch
		imageStreamLiteGenerate = oldStreamLite
		imageNowUnix = oldNowUnix
		imageResponseID = oldResponseID
	})
}
