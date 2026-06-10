package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

const (
	defaultAssetListTimeout     = 30 * time.Second
	defaultAssetDeleteTimeout   = 30 * time.Second
	defaultAssetDownloadTimeout = 120 * time.Second
)

var (
	assetsSlotsMu                  sync.Mutex
	assetListSlots                 chan struct{}
	assetDeleteSlots               chan struct{}
	assetListConcurrencyProvider   = func() int { return config.GlobalConfig.GetInt("batch.asset_list_concurrency", 50) }
	assetDeleteConcurrencyProvider = func() int { return config.GlobalConfig.GetInt("batch.asset_delete_concurrency", 50) }
	assetListTimeoutProvider       = func() float64 { return config.GlobalConfig.GetFloat("asset.list_timeout", 30.0) }
	assetDeleteTimeoutProvider     = func() float64 { return config.GlobalConfig.GetFloat("asset.delete_timeout", 30.0) }
	assetDownloadTimeoutProvider   = func() float64 { return config.GlobalConfig.GetFloat("asset.download_timeout", 120.0) }
)

type AssetsProxyRuntime interface {
	Acquire(ctx context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type AssetsHTTPClient interface {
	GetJSON(ctx context.Context, request AssetsHTTPRequest) (map[string]any, error)
	DeleteJSON(ctx context.Context, request AssetsHTTPRequest) (map[string]any, error)
	GetBytesStream(ctx context.Context, request AssetsHTTPRequest) (io.ReadCloser, error)
}

type AssetsOptions struct {
	ProxyRuntime    AssetsProxyRuntime
	Client          AssetsHTTPClient
	ListTimeout     time.Duration
	DeleteTimeout   time.Duration
	DownloadTimeout time.Duration
}

type AssetsHTTPRequest struct {
	URL          string
	Token        string
	Params       map[string]any
	Lease        *controlproxy.ProxyLease
	Timeout      time.Duration
	Origin       string
	Referer      string
	ExtraHeaders map[string]string
}

type AssetDownloadResult struct {
	Stream      io.ReadCloser
	ContentType *string
}

func ListAssets(ctx context.Context, token string, params map[string]any, options ...AssetsOptions) (map[string]any, error) {
	release, err := acquireAssetsSlot(ctx, assetListSlotChannel())
	if err != nil {
		return nil, assetsTransportError("list_assets", err)
	}
	defer release()
	option := assetsOptions(options...)
	return listAssetsInner(ctx, token, params, option)
}

func DeleteAsset(ctx context.Context, token string, assetID string, options ...AssetsOptions) (map[string]any, error) {
	release, err := acquireAssetsSlot(ctx, assetDeleteSlotChannel())
	if err != nil {
		return nil, assetsTransportError("delete_asset", err)
	}
	defer release()
	option := assetsOptions(options...)
	return deleteAssetInner(ctx, token, assetID, option)
}

func DownloadAsset(ctx context.Context, token string, filePath string, options ...AssetsOptions) (AssetDownloadResult, error) {
	option := assetsOptions(options...)
	downloadURL, origin, referer := protocol.ResolveDownloadURL(filePath)
	contentType := protocol.InferContentType(downloadURL)
	lease, err := acquireAssetsProxy(ctx, option.ProxyRuntime)
	if err != nil {
		return AssetDownloadResult{}, err
	}
	stream, err := option.Client.GetBytesStream(ctx, AssetsHTTPRequest{
		URL:          downloadURL,
		Token:        token,
		Lease:        &lease,
		Timeout:      option.DownloadTimeout,
		Origin:       origin,
		Referer:      referer,
		ExtraHeaders: assetDownloadHeaders(contentType),
	})
	if err != nil {
		return AssetDownloadResult{}, handleAssetsError(ctx, option.ProxyRuntime, lease, "download_asset", err)
	}
	feedbackAssetsSuccess(ctx, option.ProxyRuntime, lease)
	return AssetDownloadResult{Stream: stream, ContentType: contentType}, nil
}

func listAssetsInner(ctx context.Context, token string, params map[string]any, option AssetsOptions) (map[string]any, error) {
	lease, err := acquireAssetsProxy(ctx, option.ProxyRuntime)
	if err != nil {
		return nil, err
	}
	result, err := option.Client.GetJSON(ctx, AssetsHTTPRequest{
		URL:     protocol.AssetsListURL,
		Token:   token,
		Params:  params,
		Lease:   &lease,
		Timeout: option.ListTimeout,
		Origin:  "https://grok.com",
		Referer: "https://grok.com/files",
	})
	if err != nil {
		return nil, handleAssetsError(ctx, option.ProxyRuntime, lease, "list_assets", err)
	}
	feedbackAssetsSuccess(ctx, option.ProxyRuntime, lease)
	return result, nil
}

func deleteAssetInner(ctx context.Context, token string, assetID string, option AssetsOptions) (map[string]any, error) {
	lease, err := acquireAssetsProxy(ctx, option.ProxyRuntime)
	if err != nil {
		return nil, err
	}
	result, err := option.Client.DeleteJSON(ctx, AssetsHTTPRequest{
		URL:     protocol.AssetDeleteURL(assetID),
		Token:   token,
		Lease:   &lease,
		Timeout: option.DeleteTimeout,
		Origin:  "https://grok.com",
		Referer: "https://grok.com/files",
	})
	if err != nil {
		return nil, handleAssetsError(ctx, option.ProxyRuntime, lease, "delete_asset", err)
	}
	feedbackAssetsSuccess(ctx, option.ProxyRuntime, lease)
	return result, nil
}

func assetsOptions(options ...AssetsOptions) AssetsOptions {
	option := AssetsOptions{
		Client:          netHTTPAssetsClient{},
		ListTimeout:     configuredAssetListTimeout(),
		DeleteTimeout:   configuredAssetDeleteTimeout(),
		DownloadTimeout: configuredAssetDownloadTimeout(),
	}
	if len(options) > 0 {
		option = options[0]
	}
	if option.Client == nil {
		option.Client = netHTTPAssetsClient{}
	}
	if option.ListTimeout <= 0 {
		option.ListTimeout = configuredAssetListTimeout()
	}
	if option.DeleteTimeout <= 0 {
		option.DeleteTimeout = configuredAssetDeleteTimeout()
	}
	if option.DownloadTimeout <= 0 {
		option.DownloadTimeout = configuredAssetDownloadTimeout()
	}
	return option
}

func configuredAssetListTimeout() time.Duration {
	return configuredAssetsTimeout(assetListTimeoutProvider(), defaultAssetListTimeout)
}

func configuredAssetDeleteTimeout() time.Duration {
	return configuredAssetsTimeout(assetDeleteTimeoutProvider(), defaultAssetDeleteTimeout)
}

func configuredAssetDownloadTimeout() time.Duration {
	return configuredAssetsTimeout(assetDownloadTimeoutProvider(), defaultAssetDownloadTimeout)
}

func configuredAssetsTimeout(seconds float64, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}

func assetListSlotChannel() chan struct{} {
	assetsSlotsMu.Lock()
	defer assetsSlotsMu.Unlock()
	if assetListSlots == nil {
		assetListSlots = make(chan struct{}, assetsConcurrency(assetListConcurrencyProvider()))
	}
	return assetListSlots
}

func assetDeleteSlotChannel() chan struct{} {
	assetsSlotsMu.Lock()
	defer assetsSlotsMu.Unlock()
	if assetDeleteSlots == nil {
		assetDeleteSlots = make(chan struct{}, assetsConcurrency(assetDeleteConcurrencyProvider()))
	}
	return assetDeleteSlots
}

func assetsConcurrency(value int) int {
	if value < 1 {
		return 1
	}
	return value
}

func acquireAssetsSlot(ctx context.Context, slots chan struct{}) (func(), error) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func acquireAssetsProxy(ctx context.Context, runtime AssetsProxyRuntime) (controlproxy.ProxyLease, error) {
	if runtime == nil {
		return controlproxy.ProxyLease{}, assetsTransportError("assets", fmt.Errorf("proxy runtime is not configured"))
	}
	return runtime.Acquire(ctx, controlproxy.AcquireOptions{Scope: controlproxy.ProxyScopeAsset, Kind: controlproxy.RequestKindHTTP})
}

func handleAssetsError(ctx context.Context, runtime AssetsProxyRuntime, lease controlproxy.ProxyLease, label string, err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		_ = runtime.Feedback(ctx, lease, UpstreamFeedback(upstream))
		return err
	}
	_ = runtime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
	return assetsTransportError(label, err)
}

func feedbackAssetsSuccess(ctx context.Context, runtime AssetsProxyRuntime, lease controlproxy.ProxyLease) {
	status := 200
	_ = runtime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackSuccess, StatusCode: &status})
}

func assetsTransportError(label string, err error) *platform.UpstreamError {
	return platform.NewUpstreamError(fmt.Sprintf("%s: transport error: %v", label, err), 502, err.Error())
}

func assetDownloadHeaders(contentType *string) map[string]string {
	accept := "*/*"
	if contentType != nil {
		switch {
		case len(*contentType) >= len("video/") && (*contentType)[:len("video/")] == "video/":
			accept = "video/mp4,video/*,*/*;q=0.8"
		case len(*contentType) >= len("image/") && (*contentType)[:len("image/")] == "image/":
			accept = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"
		}
	}
	return map[string]string{
		"Accept":                    accept,
		"Cache-Control":             "no-cache",
		"Pragma":                    "no-cache",
		"Priority":                  "u=0, i",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}
}
