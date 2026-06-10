package openai

import (
	"context"
	"fmt"
	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
	"github.com/jiujiu532/grok2api/app/platform/config"
	"github.com/jiujiu532/grok2api/app/platform/storage"
	"regexp"
	"strings"
	"time"
)

const (
	editMaxReferences = 7
	editDefaultSize   = "1024x1024"
)

var (
	imageRatios = map[string]string{
		"1280x720":  "16:9",
		"16:9":      "16:9",
		"720x1280":  "9:16",
		"9:16":      "9:16",
		"1792x1024": "3:2",
		"3:2":       "3:2",
		"1024x1792": "2:3",
		"2:3":       "2:3",
		"1024x1024": "1:1",
		"1:1":       "1:1",
	}
	editImagePlaceholderRE = regexp.MustCompile(`(?i)@IMAGE(\d+)\b`)
	imageAppURL            = func() string {
		return strings.TrimRight(fmt.Sprint(config.GetConfig("app.app_url", "")), "/")
	}
	imagePublicProxyEnabled = func() bool {
		return imageBoolConfig("features.imagine_public_image_proxy", false)
	}
	imageDownloadBytes = func(ctx context.Context, token string, rawURL string) ([]byte, string, error) {
		return downloadImageBytes(ctx, token, rawURL)
	}
	imageSaveLocal = func(raw []byte, mime, fileID string) (string, error) {
		return storage.SaveLocalImage(raw, mime, fileID)
	}
	imageStreamImages = func(ctx context.Context, token, prompt string, options transport.ImagineOptions) ([]map[string]any, error) {
		return transport.StreamImages(ctx, token, prompt, options)
	}
	imageUploadFromInput = func(ctx context.Context, token, fileInput string) (transport.AssetUploadResult, error) {
		return transport.UploadFromInput(ctx, token, fileInput)
	}
	imageResolveUploadedAssetReference = func(token, fileID, fileURI string) (string, error) {
		return transport.ResolveUploadedAssetReference(token, fileID, fileURI)
	}
	imageCreateMediaPost = func(ctx context.Context, token, mediaType string, options transport.MediaOptions) (map[string]any, error) {
		return transport.CreateMediaPost(ctx, token, mediaType, options)
	}
	imageCollectEditImages = func(ctx context.Context, options imageCollectEditOptions) ([]imageOutput, error) {
		return collectEditImages(ctx, options)
	}
	imageStreamEditLines = func(ctx context.Context, options imageCollectEditOptions) ([]string, error) {
		return streamImageEditLines(ctx, options)
	}
	imageRunLiteBatch = func(ctx context.Context, options imageLiteBatchOptions) ([]imageOutput, error) {
		return runLiteBatch(ctx, options)
	}
	imageStreamLiteGenerate = func(ctx context.Context, token, prompt string, modeID model.ModeID) ([]string, error) {
		return streamChat(ctx, chatStreamOptions{
			Token:            token,
			ModeID:           modeID,
			Message:          "Drawing: " + prompt,
			RequestOverrides: map[string]any{"imageGenerationCount": 2},
			TimeoutSeconds:   chatTimeoutSeconds(),
		})
	}
	imageNowUnix = func() int64 {
		return time.Now().Unix()
	}
	imageResponseID = func() string {
		return MakeResponseID()
	}
	imageEnableNSFW = func() bool {
		return imageBoolConfig("features.enable_nsfw", true)
	}
)

type imageGenerationOptions struct {
	Model          string
	Prompt         string
	N              int
	Size           string
	ResponseFormat string
	Stream         bool
	ChatFormat     bool
}

type imageEditOptions struct {
	Model          string
	Messages       []map[string]any
	N              int
	Size           string
	ResponseFormat string
	Stream         bool
	ChatFormat     bool
}

type imageCollectEditOptions struct {
	Token           string
	Prompt          string
	ImageReferences []string
	ParentPostID    string
	RequestedN      int
	ResponseFormat  string
	ProgressCB      func(index int, progress int)
}

type imageLiteBatchOptions struct {
	Model          string
	Prompt         string
	N              int
	ResponseFormat string
	ProgressCB     func(index int, progress int)
}

type imageResult struct {
	IsStream     bool
	StreamFrames []string
	Response     map[string]any
}

type imageOutput struct {
	APIValue      string
	MarkdownValue string
}

type imageOutputOptions struct {
	Token          string
	URL            string
	ResponseFormat string
	BlobB64        string
}

type editReference struct {
	FileID  string
	FileURI string
	URL     string
}
