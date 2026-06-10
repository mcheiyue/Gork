package transport

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	proxyadapters "github.com/jiujiu532/grok2api/app/dataplane/proxy/adapters"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

const AssetUploadURL = "https://grok.com/rest/app-chat/upload-file"

const (
	defaultAssetUploadTimeout = 60 * time.Second
	defaultAssetFetchTimeout  = 30 * time.Second
)

var (
	assetUploadSlots               chan struct{}
	assetUploadSlotsMu             sync.Mutex
	assetUploadConcurrencyProvider = func() int {
		return config.GlobalConfig.GetInt("batch.asset_upload_concurrency", 10)
	}
	assetUploadTimeoutProvider = func() float64 {
		return config.GlobalConfig.GetFloat("asset.upload_timeout", 60.0)
	}
	xUserIDPattern = regexp.MustCompile(`(?:^|;\s*)x-userid=([^;]+)`)
)

type AssetProxyRuntime interface {
	Acquire(ctx context.Context) (*controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type AssetHTTPClient interface {
	Post(ctx context.Context, url string, headers map[string]string, body []byte, timeout time.Duration) (AssetHTTPResponse, error)
	Get(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (AssetHTTPResponse, error)
}

type AssetHTTPResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

type AssetUploadOptions struct {
	ProxyRuntime  AssetProxyRuntime
	Client        AssetHTTPClient
	UploadTimeout time.Duration
	FetchTimeout  time.Duration
}

type AssetUploadResult struct {
	FileID  string
	FileURI string
}

func ParseDataURI(dataURI string) (string, string, string, error) {
	if !strings.HasPrefix(dataURI, "data:") {
		return "", "", "", platform.NewValidationError("File input must be a URL or data URI", "content", "")
	}
	header, content, ok := strings.Cut(dataURI, ",")
	if !ok {
		return "", "", "", platform.NewValidationError("Malformed data URI: missing comma separator", "content", "")
	}
	if !strings.Contains(header, ";base64") {
		return "", "", "", platform.NewValidationError("Data URI must be base64-encoded", "content", "")
	}
	mime := strings.TrimSpace(strings.SplitN(header[5:], ";", 2)[0])
	if mime == "" {
		mime = "application/octet-stream"
	}
	content = stripWhitespace(content)
	if content == "" {
		return "", "", "", platform.NewValidationError("Data URI has empty payload", "content", "")
	}
	ext := "bin"
	if slash := strings.LastIndex(mime, "/"); slash >= 0 {
		ext = mime[slash+1:]
	}
	return "file." + ext, content, mime, nil
}

func UploadFile(ctx context.Context, token string, filename string, mime string, b64 string, options ...AssetUploadOptions) (AssetUploadResult, error) {
	release, err := acquireAssetUploadSlot(ctx)
	if err != nil {
		return AssetUploadResult{}, assetTransportError("Asset upload transport error", err)
	}
	defer release()
	option := assetUploadOptions(options...)
	return uploadFileInner(ctx, token, filename, mime, b64, option)
}

func UploadFromInput(ctx context.Context, token string, fileInput string, options ...AssetUploadOptions) (AssetUploadResult, error) {
	option := assetUploadOptions(options...)
	if isAssetURL(fileInput) {
		return uploadFromURL(ctx, token, fileInput, option)
	}
	filename, b64, mime, err := ParseDataURI(fileInput)
	if err != nil {
		return AssetUploadResult{}, err
	}
	return UploadFile(ctx, token, filename, mime, b64, option)
}

func ResolveUploadedAssetReference(token string, fileID string, fileURI string) (string, error) {
	userID := extractUserID(token)
	resolved := protocol.ResolveAssetReference(fileID, fileURI, userID)
	if resolved != nil {
		return *resolved, nil
	}
	return "", platform.NewUpstreamError("Could not resolve uploaded asset reference URL", 502, "")
}

func uploadFileInner(ctx context.Context, token string, filename string, mime string, b64 string, option AssetUploadOptions) (AssetUploadResult, error) {
	lease, err := acquireAssetProxy(ctx, option.ProxyRuntime, "Asset upload transport error")
	if err != nil {
		return AssetUploadResult{}, err
	}
	payload, err := uploadPayload(filename, mime, b64)
	if err != nil {
		feedbackAssetTransport(ctx, option.ProxyRuntime, lease)
		return AssetUploadResult{}, assetTransportError("Asset upload transport error", err)
	}
	headers := proxyadapters.BuildHTTPHeaders(token, proxyadapters.HTTPHeaderOptions{Lease: lease})
	response, err := option.Client.Post(ctx, AssetUploadURL, headers, payload, option.UploadTimeout)
	if err != nil {
		feedbackAssetTransport(ctx, option.ProxyRuntime, lease)
		return AssetUploadResult{}, assetTransportError("Asset upload transport error", err)
	}
	if response.StatusCode != 200 {
		return AssetUploadResult{}, handleUploadHTTPFailure(ctx, option.ProxyRuntime, lease, response)
	}
	feedbackAssetSuccess(ctx, option.ProxyRuntime, lease, true)
	return parseUploadResponse(ctx, option.ProxyRuntime, lease, response.Body)
}

func uploadFromURL(ctx context.Context, token string, fileURL string, option AssetUploadOptions) (AssetUploadResult, error) {
	lease, err := acquireAssetProxy(ctx, option.ProxyRuntime, "Asset fetch transport error")
	if err != nil {
		return AssetUploadResult{}, err
	}
	headers := proxyadapters.BuildHTTPHeaders(token, proxyadapters.HTTPHeaderOptions{Lease: lease})
	response, err := option.Client.Get(ctx, fileURL, headers, option.FetchTimeout)
	if err != nil {
		feedbackAssetTransport(ctx, option.ProxyRuntime, lease)
		return AssetUploadResult{}, assetTransportError("Asset fetch transport error", err)
	}
	if response.StatusCode != 200 {
		return AssetUploadResult{}, handleFetchHTTPFailure(ctx, option.ProxyRuntime, lease, response.StatusCode)
	}
	feedbackAssetSuccess(ctx, option.ProxyRuntime, lease, false)
	mime := responseContentType(response.Headers)
	filename := filenameFromURL(fileURL)
	b64 := base64.StdEncoding.EncodeToString(response.Body)
	return UploadFile(ctx, token, filename, mime, b64, option)
}

func assetUploadOptions(options ...AssetUploadOptions) AssetUploadOptions {
	option := AssetUploadOptions{Client: netHTTPAssetClient{}, UploadTimeout: configuredAssetUploadTimeout(), FetchTimeout: defaultAssetFetchTimeout}
	if len(options) > 0 {
		option = options[0]
	}
	if option.Client == nil {
		option.Client = netHTTPAssetClient{}
	}
	if option.UploadTimeout <= 0 {
		option.UploadTimeout = defaultAssetUploadTimeout
	}
	if option.FetchTimeout <= 0 {
		option.FetchTimeout = defaultAssetFetchTimeout
	}
	return option
}

func configuredAssetUploadTimeout() time.Duration {
	seconds := assetUploadTimeoutProvider()
	if seconds <= 0 {
		return defaultAssetUploadTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}

func acquireAssetUploadSlot(ctx context.Context) (func(), error) {
	slots := assetUploadSlotChannel()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func assetUploadSlotChannel() chan struct{} {
	assetUploadSlotsMu.Lock()
	defer assetUploadSlotsMu.Unlock()
	if assetUploadSlots == nil {
		assetUploadSlots = make(chan struct{}, assetUploadConcurrency())
	}
	return assetUploadSlots
}

func assetUploadConcurrency() int {
	concurrency := assetUploadConcurrencyProvider()
	if concurrency < 1 {
		return 1
	}
	return concurrency
}

func acquireAssetProxy(ctx context.Context, runtime AssetProxyRuntime, label string) (*controlproxy.ProxyLease, error) {
	if runtime == nil {
		return nil, assetTransportError(label, fmt.Errorf("proxy runtime is not configured"))
	}
	lease, err := runtime.Acquire(ctx)
	if err != nil {
		return nil, assetTransportError(label, err)
	}
	return lease, nil
}

func uploadPayload(filename string, mime string, b64 string) ([]byte, error) {
	return json.Marshal(struct {
		FileName     string `json:"fileName"`
		FileMimeType string `json:"fileMimeType"`
		Content      string `json:"content"`
	}{FileName: filename, FileMimeType: mime, Content: b64})
}

func handleUploadHTTPFailure(ctx context.Context, runtime AssetProxyRuntime, lease *controlproxy.ProxyLease, response AssetHTTPResponse) error {
	bodyText := truncateString(string(response.Body), 300)
	isCloudflare := strings.Contains(strings.ToLower(bodyText), "just a moment")
	_ = runtime.Feedback(ctx, *lease, controlproxy.BuildFeedback(response.StatusCode, controlproxy.BuildFeedbackOptions{IsCloudflare: isCloudflare}))
	return platform.NewUpstreamError(fmt.Sprintf("Asset upload returned %d", response.StatusCode), response.StatusCode, bodyText)
}

func handleFetchHTTPFailure(ctx context.Context, runtime AssetProxyRuntime, lease *controlproxy.ProxyLease, statusCode int) error {
	kind := controlproxy.ProxyFeedbackForbidden
	if statusCode >= 500 {
		kind = controlproxy.ProxyFeedbackUpstream5xx
	}
	_ = runtime.Feedback(ctx, *lease, controlproxy.ProxyFeedback{Kind: kind, StatusCode: &statusCode})
	return platform.NewUpstreamError(fmt.Sprintf("Failed to fetch input URL: %d", statusCode), statusCode, "")
}

func parseUploadResponse(ctx context.Context, runtime AssetProxyRuntime, lease *controlproxy.ProxyLease, body []byte) (AssetUploadResult, error) {
	var parsed struct {
		FileMetadataID string `json:"fileMetadataId"`
		FileID         string `json:"fileId"`
		FileURI        string `json:"fileUri"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		feedbackAssetTransport(ctx, runtime, lease)
		return AssetUploadResult{}, assetTransportError("Asset upload transport error", err)
	}
	fileID := parsed.FileMetadataID
	if fileID == "" {
		fileID = parsed.FileID
	}
	return AssetUploadResult{FileID: fileID, FileURI: parsed.FileURI}, nil
}

func feedbackAssetSuccess(ctx context.Context, runtime AssetProxyRuntime, lease *controlproxy.ProxyLease, includeStatus bool) {
	feedback := controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackSuccess}
	if includeStatus {
		status := 200
		feedback.StatusCode = &status
	}
	_ = runtime.Feedback(ctx, *lease, feedback)
}

func feedbackAssetTransport(ctx context.Context, runtime AssetProxyRuntime, lease *controlproxy.ProxyLease) {
	if runtime == nil || lease == nil {
		return
	}
	_ = runtime.Feedback(ctx, *lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
}

func assetTransportError(label string, err error) *platform.UpstreamError {
	return platform.NewUpstreamError(fmt.Sprintf("%s: %v", label, err), 502, err.Error())
}
