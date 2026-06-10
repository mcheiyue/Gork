package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	reverseruntime "github.com/jiujiu532/grok2api/app/dataplane/reverse/runtime"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/config"
	"github.com/jiujiu532/grok2api/app/platform/storage"
)

const (
	imageMediaType        = "MEDIA_POST_TYPE_IMAGE"
	videoMediaType        = "MEDIA_POST_TYPE_VIDEO"
	videoModelName        = "imagine-video-gen"
	videoExtensionRefType = "ORIGINAL_REF_TYPE_VIDEO_EXTENSION"
)

type videoReference struct {
	ContentURL string
	PostID     string
}

var (
	videoCreateMediaPost = func(ctx context.Context, token, mediaType string, options transport.MediaOptions) (map[string]any, error) {
		return transport.CreateMediaPost(ctx, token, mediaType, options)
	}
	videoUploadFromInput = func(ctx context.Context, token, fileInput string) (transport.AssetUploadResult, error) {
		return transport.UploadFromInput(ctx, token, fileInput)
	}
	videoResolveUploadedAssetReference = func(token, fileID, fileURI string) (string, error) {
		return transport.ResolveUploadedAssetReference(token, fileID, fileURI)
	}
	videoStreamLines   = defaultVideoStreamLines
	videoDownloadBytes = func(ctx context.Context, token, rawURL string) ([]byte, string, error) {
		result, err := transport.DownloadAsset(ctx, token, rawURL)
		if err != nil {
			return nil, "", err
		}
		defer result.Stream.Close()
		raw, err := io.ReadAll(result.Stream)
		if err != nil {
			return nil, "", err
		}
		mime := "video/mp4"
		if result.ContentType != nil && *result.ContentType != "" {
			mime = *result.ContentType
		}
		return raw, mime, nil
	}
	videoSaveLocal = func(raw []byte, fileID string) (string, error) {
		return storage.SaveLocalVideo(raw, fileID)
	}
	videoFormatConfig = func() string {
		return fmt.Sprint(config.GetConfig("features.video_format", "grok_url"))
	}
	videoAppURL = func() string {
		return strings.TrimRight(fmt.Sprint(config.GetConfig("app.app_url", "")), "/")
	}
)

func defaultVideoGenerate(ctx context.Context, options videoGenerateOptions) (VideoArtifact, error) {
	spec, err := model.Resolve(options.Model)
	if err != nil {
		return VideoArtifact{}, err
	}
	if !spec.IsVideo() {
		return VideoArtifact{}, platform.NewValidationError("Model '"+options.Model+"' is not a video model", "model", "")
	}
	aspectRatio, defaultResolution, err := resolveVideoSize(options.Size)
	if err != nil {
		return VideoArtifact{}, err
	}
	resolution, err := resolveVideoResolutionName(options.ResolutionName, defaultResolution)
	if err != nil {
		return VideoArtifact{}, err
	}
	preset, err := resolveVideoPreset(options.Preset, "custom")
	if err != nil {
		return VideoArtifact{}, err
	}
	directory := chatDirectoryProvider()
	if directory == nil {
		return VideoArtifact{}, platform.NewRateLimitError("Account directory not initialised")
	}
	account, ok, err := directory.ReserveChatAccount(ctx, spec, nil)
	if err != nil {
		return VideoArtifact{}, err
	}
	if !ok {
		return VideoArtifact{}, platform.NewRateLimitError("No available accounts for video generation")
	}
	success := false
	var failErr error
	defer func() {
		_ = directory.ReleaseChatAccount(ctx, account)
		if success || failErr == nil {
			return
		}
		kind := feedbackKind(failErr)
		if kind == feedbackKindUnauthorized || kind == feedbackKindForbidden {
			_ = directory.FeedbackChatAccount(ctx, chatFeedback{Token: account.Token, Kind: kind, ModeID: account.ModeID})
		}
	}()
	artifact, err := generateVideoWithToken(ctx, account.Token, videoTokenOptions{
		Prompt:          options.Prompt,
		AspectRatio:     aspectRatio,
		ResolutionName:  resolution,
		Seconds:         options.Seconds,
		Preset:          preset,
		InputReferences: options.InputReferences,
		ProgressCB:      options.ProgressCB,
		TimeoutSeconds:  chatTimeoutSeconds(),
	})
	if err != nil {
		failErr = err
		return VideoArtifact{}, err
	}
	if artifact.VideoURL != "" {
		fileID := firstNonEmpty(artifact.AssetID, artifact.VideoPostID, MakeResponseID())
		localPath, err := downloadAndSaveVideo(ctx, account.Token, artifact.VideoURL, fileID)
		if err != nil {
			failErr = err
			return VideoArtifact{}, err
		}
		artifact.LocalContentFilePath = localPath
	}
	success = true
	return artifact, nil
}

type videoTokenOptions struct {
	Prompt          string
	AspectRatio     string
	ResolutionName  string
	Seconds         int
	Preset          string
	InputReferences []map[string]any
	ProgressCB      func(int)
	TimeoutSeconds  float64
}

func generateVideoWithToken(ctx context.Context, token string, options videoTokenOptions) (VideoArtifact, error) {
	references, err := prepareVideoReferences(ctx, token, options.InputReferences)
	if err != nil {
		return VideoArtifact{}, err
	}
	parentPostID := ""
	if len(references) > 0 {
		parentPostID = references[0].PostID
	} else {
		post, err := videoCreateMediaPost(ctx, token, videoMediaType, transport.MediaOptions{
			Prompt:  options.Prompt,
			Referer: "https://grok.com/imagine",
		})
		if err != nil {
			return VideoArtifact{}, err
		}
		parentPostID = nestedString(post, "post", "id")
		if parentPostID == "" {
			return VideoArtifact{}, platform.NewUpstreamError("Video create-post returned no post id", 502, "")
		}
	}
	segments := buildVideoSegmentLengths(options.Seconds)
	var artifact VideoArtifact
	extendPostID := parentPostID
	elapsedSeconds := 0
	for index, segmentLength := range segments {
		var payload map[string]any
		referer := "https://grok.com/imagine"
		if index == 0 {
			payload = videoCreatePayload(options.Prompt, parentPostID, options.AspectRatio, options.ResolutionName, segmentLength, options.Preset, referenceContentURLs(references))
		} else {
			payload = videoExtendPayload(options.Prompt, parentPostID, extendPostID, options.AspectRatio, options.ResolutionName, segmentLength, options.Preset, videoExtendStartTime(elapsedSeconds))
			referer = "https://grok.com/imagine/post/" + parentPostID
		}
		progressCB := func(progress int) {
			if options.ProgressCB == nil {
				return
			}
			scaled := int(((float64(index) + (float64(clampInt(progress, 0, 100)) / 100.0)) / float64(len(segments))) * 100)
			options.ProgressCB(scaled)
		}
		artifact, err = collectVideoSegment(ctx, token, payload, referer, options.TimeoutSeconds, progressCB)
		if err != nil {
			return VideoArtifact{}, err
		}
		if index == 0 && len(segments) > 1 {
			if artifact.VideoPostID != "" {
				artifact.RemixedFromVideoID = artifact.VideoPostID
			} else {
				artifact.RemixedFromVideoID = parentPostID
			}
		}
		extendPostID = firstNonEmpty(artifact.VideoPostID, artifact.AssetID, parentPostID)
		elapsedSeconds += segmentLength
	}
	if artifact.VideoURL == "" {
		return VideoArtifact{}, platform.NewUpstreamError("Video generation returned no artifact", 502, "")
	}
	return artifact, nil
}

func prepareVideoReferences(ctx context.Context, token string, inputReferences []map[string]any) ([]videoReference, error) {
	if len(inputReferences) == 0 {
		return nil, nil
	}
	references := make([]videoReference, 0, len(inputReferences))
	for index, ref := range inputReferences {
		reference, err := prepareVideoReference(ctx, token, ref)
		if err != nil {
			return nil, wrapVideoReferenceError(index, err)
		}
		references = append(references, reference)
	}
	return references, nil
}

func prepareVideoReference(ctx context.Context, token string, inputReference map[string]any) (videoReference, error) {
	fileID := strings.TrimSpace(stringValue(inputReference["file_id"], ""))
	imageInput := strings.TrimSpace(stringValue(inputReference["image_url"], ""))
	if fileID != "" && imageInput != "" {
		return videoReference{}, platform.NewValidationError("input_reference accepts only one of file_id or image_url", "input_reference", "")
	}
	if fileID != "" {
		return videoReference{}, platform.NewValidationError("input_reference.file_id is not supported yet", "input_reference.file_id", "")
	}
	if imageInput == "" {
		return videoReference{}, platform.NewValidationError("input_reference.image_url is required", "input_reference.image_url", "")
	}
	contentURL := imageInput
	if !isUpstreamAssetContentURL(imageInput) {
		upload, err := videoUploadFromInput(ctx, token, imageInput)
		if err != nil {
			return videoReference{}, platform.NewUpstreamError("Video input reference upload failed: "+err.Error(), 502, "")
		}
		resolved, err := videoResolveUploadedAssetReference(token, upload.FileID, upload.FileURI)
		if err != nil {
			return videoReference{}, err
		}
		contentURL = resolved
	}
	post, err := videoCreateMediaPost(ctx, token, imageMediaType, transport.MediaOptions{
		MediaURL: contentURL,
		Prompt:   "",
		Referer:  "https://grok.com/imagine",
	})
	if err != nil {
		return videoReference{}, err
	}
	postID := nestedString(post, "post", "id")
	if postID == "" {
		return videoReference{}, platform.NewUpstreamError("Video image reference create-post returned no post id", 502, "")
	}
	return videoReference{ContentURL: contentURL, PostID: postID}, nil
}

func wrapVideoReferenceError(index int, err error) error {
	message := fmt.Sprintf("Video input reference %d failed: %v", index+1, err)
	var appErr *platform.AppError
	if errors.As(err, &appErr) && appErr.Kind == platform.ErrorKindValidation {
		return platform.NewValidationError(message, "input_reference", "")
	}
	return platform.NewUpstreamError(message, 502, "")
}

func videoCreatePayload(prompt, parentPostID, aspectRatio, resolutionName string, videoLength int, preset string, imageReferences []string) map[string]any {
	videoConfig := map[string]any{
		"parentPostId":   parentPostID,
		"aspectRatio":    aspectRatio,
		"videoLength":    videoLength,
		"resolutionName": resolutionName,
	}
	if len(imageReferences) > 0 {
		videoConfig["isVideoEdit"] = false
		videoConfig["isReferenceToVideo"] = true
		videoConfig["imageReferences"] = imageReferences
	}
	return map[string]any{
		"temporary":        true,
		"modelName":        videoModelName,
		"message":          buildVideoMessage(prompt, preset),
		"enableSideBySide": true,
		"responseMetadata": map[string]any{
			"experiments": []any{},
			"modelConfigOverride": map[string]any{
				"modelMap": map[string]any{
					"videoGenModelConfig": videoConfig,
				},
			},
		},
	}
}

func videoExtendPayload(prompt, parentPostID, extendPostID, aspectRatio, resolutionName string, videoLength int, preset string, startTimeS float64) map[string]any {
	return map[string]any{
		"temporary":        true,
		"modelName":        videoModelName,
		"message":          buildVideoMessage(prompt, preset),
		"enableSideBySide": true,
		"responseMetadata": map[string]any{
			"experiments": []any{},
			"modelConfigOverride": map[string]any{
				"modelMap": map[string]any{
					"videoGenModelConfig": map[string]any{
						"isVideoExtension":        true,
						"videoExtensionStartTime": startTimeS,
						"extendPostId":            extendPostID,
						"stitchWithExtendPostId":  true,
						"originalPrompt":          prompt,
						"originalPostId":          parentPostID,
						"originalRefType":         videoExtensionRefType,
						"mode":                    preset,
						"aspectRatio":             aspectRatio,
						"videoLength":             videoLength,
						"resolutionName":          resolutionName,
						"parentPostId":            parentPostID,
						"isVideoEdit":             false,
					},
				},
			},
		},
	}
}

func defaultVideoStreamLines(ctx context.Context, token string, payload map[string]any, referer string, timeoutS float64) ([]string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	stream, err := transport.PostStream(ctx, reverseruntime.Chat, token, raw, transport.HTTPOptions{
		Timeout:     secondsDuration(timeoutS, 120*time.Second),
		ContentType: "application/json",
		Origin:      "https://grok.com",
		Referer:     referer,
	})
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func collectVideoSegment(ctx context.Context, token string, payload map[string]any, referer string, timeoutS float64, progressCB func(int)) (VideoArtifact, error) {
	lines, err := videoStreamLines(ctx, token, payload, referer, timeoutS)
	if err != nil {
		return VideoArtifact{}, err
	}
	finalURL := ""
	finalAssetID := ""
	finalThumbnail := ""
	videoPostID := ""
	streamData := []string{}
	for _, line := range lines {
		eventType, data := protocol.ClassifyLine(line)
		if eventType == "done" {
			break
		}
		if eventType != "data" || data == "" {
			continue
		}
		streamData = append(streamData, data)
		obj := map[string]any{}
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			continue
		}
		if err := protocol.StreamErrorFromPayload(obj); err != nil {
			return VideoArtifact{}, err
		}
		if stream := nestedMap(obj, "result", "response", "streamingVideoGenerationResponse"); stream != nil {
			progress := intFromAny(stream["progress"])
			if progressCB != nil {
				progressCB(progress)
			}
			videoPostID = firstNonEmpty(stringValue(stream["videoPostId"], ""), stringValue(stream["videoId"], ""), videoPostID)
			if progress >= 100 && stream["moderated"] != true {
				if rawURL := stringValue(stream["videoUrl"], ""); rawURL != "" {
					finalURL = absolutizeVideoURL(rawURL)
				}
				if assetID := stringValue(stream["assetId"], ""); assetID != "" {
					finalAssetID = assetID
				}
				if thumbnail := stringValue(stream["thumbnailImageUrl"], ""); thumbnail != "" {
					finalThumbnail = absolutizeVideoURL(thumbnail)
				}
			}
		}
		if attachments := extractVideoFileAttachments(obj); len(attachments) > 0 && finalAssetID == "" {
			finalAssetID = attachments[0]
		}
	}
	if finalURL == "" && finalAssetID != "" {
		if resolved := protocol.ResolveAssetReference(finalAssetID, "", ""); resolved != nil {
			finalURL = *resolved
		}
	}
	if finalURL == "" && finalAssetID != "" {
		return VideoArtifact{}, platform.NewUpstreamError("Video segment returned only assetId without a resolvable URL", 502, strings.Join(streamData, "\n"))
	}
	if finalURL == "" {
		return VideoArtifact{}, platform.NewUpstreamError("Video generation returned no final video URL", 502, strings.Join(streamData, "\n"))
	}
	return VideoArtifact{VideoURL: finalURL, VideoPostID: firstNonEmpty(videoPostID, finalAssetID), AssetID: finalAssetID, ThumbnailURL: finalThumbnail}, nil
}

func downloadAndSaveVideo(ctx context.Context, token, rawURL, fileID string) (string, error) {
	raw, _, err := videoDownloadBytes(ctx, token, rawURL)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", platform.NewUpstreamError("Video download returned empty content", 502, "")
	}
	trimmed := strings.TrimLeft(string(raw[:minInt(len(raw), 16)]), " \t\r\n")
	if strings.HasPrefix(trimmed, "<") || strings.HasPrefix(trimmed, "{") {
		return "", platform.NewUpstreamError("Video download returned non-video content", 502, "")
	}
	return videoSaveLocal(raw, fileID)
}

func buildVideoMessage(prompt, preset string) string {
	flag := videoPresetFlags[preset]
	if flag == "" {
		flag = "--mode=custom"
	}
	return strings.TrimSpace(prompt + " " + flag)
}

func videoExtendStartTime(seconds int) float64 {
	return float64(seconds) + 1.0/24.0
}

func absolutizeVideoURL(rawURL string) string {
	fullURL, _, _ := protocol.ResolveDownloadURL(rawURL)
	return fullURL
}

func isUpstreamAssetContentURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" && parsed.Host == "assets.grok.com" && strings.HasSuffix(parsed.Path, "/content")
}

func extractVideoFileAttachments(obj map[string]any) []string {
	modelResponse := nestedMap(obj, "result", "response", "modelResponse")
	if modelResponse == nil {
		return nil
	}
	attachments, _ := modelResponse["fileAttachments"].([]any)
	result := []string{}
	for _, item := range attachments {
		if text := stringValue(item, ""); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func referenceContentURLs(references []videoReference) []string {
	if len(references) == 0 {
		return nil
	}
	result := make([]string, 0, len(references))
	for _, reference := range references {
		result = append(result, reference.ContentURL)
	}
	return result
}

func nestedMap(value map[string]any, keys ...string) map[string]any {
	current := value
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func nestedString(value map[string]any, keys ...string) string {
	current := nestedMap(value, keys[:len(keys)-1]...)
	if current == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(current[keys[len(keys)-1]], ""))
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func renderVideoHTML(rawURL string) string {
	return `<video controls src="` + html.EscapeString(rawURL) + `"></video>`
}

func localVideoURL(fileID string) string {
	appURL := videoAppURL()
	if appURL == "" {
		return "/v1/files/video?id=" + fileID
	}
	return appURL + "/v1/files/video?id=" + fileID
}

func normalizeVideoFormat(value string) (string, error) {
	format := strings.ToLower(strings.TrimSpace(value))
	if format == "" {
		format = "grok_url"
	}
	switch format {
	case "grok_url", "local_url", "grok_html", "local_html":
		return format, nil
	default:
		return "", platform.NewValidationError("video_format must be one of [grok_url, local_url, grok_html, local_html]", "features.video_format", "")
	}
}

func resolveVideoOutput(ctx context.Context, token, rawURL, fileID string) (string, string) {
	format, err := normalizeVideoFormat(videoFormatConfig())
	if err != nil || format == "grok_url" {
		return rawURL, ""
	}
	if format == "grok_html" {
		return renderVideoHTML(rawURL), ""
	}
	path, err := downloadAndSaveVideo(ctx, token, rawURL, fileID)
	if err != nil {
		if format == "local_html" {
			return renderVideoHTML(rawURL), ""
		}
		return rawURL, ""
	}
	localURL := localVideoURL(fileID)
	if format == "local_html" {
		return renderVideoHTML(localURL), path
	}
	return localURL, path
}

func secondsDuration(seconds float64, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}
