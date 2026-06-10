package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	"github.com/jiujiu532/grok2api/app/platform"
)

func TestVideoValidationHelpersMatchPython(t *testing.T) {
	if err := ValidateVideoLength(12); err != nil {
		t.Fatalf("12s validation err=%v", err)
	}
	if err := ValidateVideoLength(8); !isValidationParam(err, "seconds") {
		t.Fatalf("8s validation err=%#v", err)
	}
	if got, want := buildVideoSegmentLengths(16), []int{10, 6}; !reflect.DeepEqual(got, want) {
		t.Fatalf("segments=%v want %v", got, want)
	}
	aspect, resolution, err := resolveVideoSize("1280x720")
	if err != nil || aspect != "16:9" || resolution != "720p" {
		t.Fatalf("size aspect=%q resolution=%q err=%v", aspect, resolution, err)
	}
	if _, _, err := resolveVideoSize("1x1"); !isValidationParam(err, "size") {
		t.Fatalf("size err=%#v", err)
	}
	if got, err := resolveVideoPreset(" FUN ", "custom"); err != nil || got != "fun" {
		t.Fatalf("preset=%q err=%v", got, err)
	}
}

func TestVideoCreateRetrieveAndContentPath(t *testing.T) {
	resetVideoDepsForTest(t)
	t.Setenv("DATA_DIR", t.TempDir())
	videoStartJob = func(context.Context, *VideoJob, videoJobOptions) {}
	scheduledVideoID := ""
	videoScheduleExpiration = func(videoID string) { scheduledVideoID = videoID }

	body, err := CreateVideo(context.Background(), VideoCreateOptions{
		Model:   "grok-imagine-video",
		Prompt:  "  make a clip  ",
		Seconds: 12,
		Size:    "1280x720",
	})
	if err != nil {
		t.Fatalf("create err=%v", err)
	}
	videoID := body["id"].(string)
	if !strings.HasPrefix(videoID, "video_") || body["object"] != "video" || body["status"] != "queued" {
		t.Fatalf("create body=%#v", body)
	}
	if body["prompt"] != "make a clip" || body["seconds"] != "12" || body["quality"] != "standard" {
		t.Fatalf("normalized body=%#v", body)
	}
	if scheduledVideoID != videoID {
		t.Fatalf("scheduled video id=%q want %q", scheduledVideoID, videoID)
	}

	retrieved, err := RetrieveVideo(videoID)
	if err != nil {
		t.Fatalf("retrieve err=%v", err)
	}
	if retrieved["id"] != videoID {
		t.Fatalf("retrieved=%#v", retrieved)
	}
	var appErr *platform.AppError
	if _, err := VideoContentPath(videoID); !errors.As(err, &appErr) || appErr.Status != http.StatusConflict {
		t.Fatalf("content before ready err=%#v", err)
	}

	path := filepath.Join(t.TempDir(), videoID+".mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o644); err != nil {
		t.Fatal(err)
	}
	job, _ := GetVideoJob(videoID)
	job.Status = "completed"
	job.ContentPath = path
	gotPath, err := VideoContentPath(videoID)
	if err != nil || gotPath != path {
		t.Fatalf("content path=%q err=%v", gotPath, err)
	}
}

func TestRouterVideoEndpointsUseVideoJobs(t *testing.T) {
	resetRouterDepsForTest(t)
	resetVideoDepsForTest(t)
	videoStartJob = func(context.Context, *VideoJob, videoJobOptions) {}

	form := strings.NewReader("model=grok-imagine-video&prompt=make+video&seconds=6&size=720x1280")
	req := httptest.NewRequest(http.MethodPost, "/v1/videos", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	videoID := decodeRouterJSON(t, rec)["id"].(string)

	rec = httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/videos/"+videoID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("retrieve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if decodeRouterJSON(t, rec)["id"] != videoID {
		t.Fatalf("retrieve body=%s", rec.Body.String())
	}
}

func TestVideoDefaultGenerateUsesProductionHooksAndSavesContent(t *testing.T) {
	resetVideoDepsForTest(t)
	oldDirectory := chatDirectoryProvider
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok-video", ModeID: model.ModeAuto}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	t.Cleanup(func() { chatDirectoryProvider = oldDirectory })

	var mediaTypes []string
	videoCreateMediaPost = func(_ context.Context, token, mediaType string, options transport.MediaOptions) (map[string]any, error) {
		if token != "tok-video" {
			t.Fatalf("token=%q", token)
		}
		mediaTypes = append(mediaTypes, mediaType)
		if mediaType == imageMediaType && options.MediaURL == "" {
			t.Fatalf("image reference media url empty")
		}
		return map[string]any{"post": map[string]any{"id": "post-" + mediaType}}, nil
	}
	videoStreamLines = func(_ context.Context, token string, payload map[string]any, referer string, timeoutS float64) ([]string, error) {
		if token != "tok-video" || referer != "https://grok.com/imagine" || timeoutS <= 0 {
			t.Fatalf("stream token/referer/timeout=%q/%q/%v", token, referer, timeoutS)
		}
		if payload["modelName"] != videoModelName || !strings.Contains(payload["message"].(string), "--mode=custom") {
			t.Fatalf("payload=%#v", payload)
		}
		return []string{
			`data: {"result":{"response":{"streamingVideoGenerationResponse":{"progress":100,"videoPostId":"post-video","assetId":"asset-video","videoUrl":"/users/u/video.mp4/content","thumbnailImageUrl":"/users/u/thumb.jpg/content"}}}}`,
			"data: [DONE]",
		}, nil
	}
	videoDownloadBytes = func(_ context.Context, token, rawURL string) ([]byte, string, error) {
		if token != "tok-video" || rawURL != "https://assets.grok.com/users/u/video.mp4/content" {
			t.Fatalf("download token/url=%q/%q", token, rawURL)
		}
		return []byte{0, 0, 0, 'm', 'p', '4'}, "video/mp4", nil
	}
	videoSaveLocal = func(raw []byte, fileID string) (string, error) {
		if string(raw[3:]) != "mp4" || fileID != "asset-video" {
			t.Fatalf("save raw/fileID=%v/%q", raw, fileID)
		}
		return filepath.Join(t.TempDir(), fileID+".mp4"), nil
	}

	artifact, err := videoGenerate(context.Background(), videoGenerateOptions{
		Model:          "grok-imagine-video",
		Prompt:         "make a clip",
		Seconds:        6,
		Size:           "720x1280",
		ResolutionName: "720p",
		Preset:         "custom",
	})
	if err != nil {
		t.Fatalf("generate err=%v", err)
	}
	if artifact.VideoURL != "https://assets.grok.com/users/u/video.mp4/content" ||
		artifact.ThumbnailURL != "https://assets.grok.com/users/u/thumb.jpg/content" ||
		artifact.AssetID != "asset-video" ||
		!strings.HasSuffix(artifact.LocalContentFilePath, "asset-video.mp4") {
		t.Fatalf("artifact=%#v", artifact)
	}
	if !reflect.DeepEqual(mediaTypes, []string{videoMediaType}) {
		t.Fatalf("media types=%v", mediaTypes)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 0 {
		t.Fatalf("release/feedback=%d/%#v", dir.releases, dir.feedbacks)
	}
}

func resetVideoDepsForTest(t *testing.T) {
	t.Helper()
	oldStartJob := videoStartJob
	oldScheduleExpiration := videoScheduleExpiration
	oldGenerate := videoGenerate
	oldCreateMediaPost := videoCreateMediaPost
	oldUploadFromInput := videoUploadFromInput
	oldResolveUploadedAssetReference := videoResolveUploadedAssetReference
	oldStreamLines := videoStreamLines
	oldDownloadBytes := videoDownloadBytes
	oldSaveLocal := videoSaveLocal
	oldFormatConfig := videoFormatConfig
	oldAppURL := videoAppURL
	oldNow := videoNowUnix
	oldID := videoID
	clearVideoJobs()
	videoNowUnix = func() int64 { return 1234 }
	videoID = func() string { return "video_testid" }
	videoScheduleExpiration = func(string) {}
	t.Cleanup(func() {
		videoStartJob = oldStartJob
		videoScheduleExpiration = oldScheduleExpiration
		videoGenerate = oldGenerate
		videoCreateMediaPost = oldCreateMediaPost
		videoUploadFromInput = oldUploadFromInput
		videoResolveUploadedAssetReference = oldResolveUploadedAssetReference
		videoStreamLines = oldStreamLines
		videoDownloadBytes = oldDownloadBytes
		videoSaveLocal = oldSaveLocal
		videoFormatConfig = oldFormatConfig
		videoAppURL = oldAppURL
		videoNowUnix = oldNow
		videoID = oldID
		clearVideoJobs()
	})
}
