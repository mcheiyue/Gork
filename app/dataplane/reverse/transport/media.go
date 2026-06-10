package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

const defaultMediaTimeout = 60 * time.Second

var mediaTimeoutProvider = func(defaultSeconds float64) float64 {
	return config.GlobalConfig.GetFloat("video.timeout", defaultSeconds)
}

type MediaProxyRuntime interface {
	Acquire(ctx context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type MediaHTTPClient interface {
	PostJSON(ctx context.Context, request MediaHTTPRequest) (map[string]any, error)
}

type MediaHTTPRequest struct {
	URL     string
	Token   string
	Payload []byte
	Lease   *controlproxy.ProxyLease
	Timeout time.Duration
	Origin  string
	Referer string
}

type MediaOptions struct {
	ProxyRuntime MediaProxyRuntime
	Client       MediaHTTPClient
	Timeout      time.Duration
	Referer      string
	MediaURL     string
	Prompt       string
}

func CreateMediaPost(ctx context.Context, token, mediaType string, options MediaOptions) (map[string]any, error) {
	referer := options.Referer
	if referer == "" {
		referer = "https://grok.com/imagine"
	}
	return postMediaWithProxy(ctx, protocol.MediaPostURL, token, protocol.BuildMediaPostPayload(protocol.MediaPostPayloadOptions{
		MediaType: mediaType,
		MediaURL:  options.MediaURL,
		Prompt:    options.Prompt,
	}), "create_media_post", referer, options)
}

func CreateMediaLink(ctx context.Context, token, postID string, options MediaOptions) (map[string]any, error) {
	return postMediaWithProxy(ctx, protocol.MediaLinkURL, token, protocol.BuildMediaLinkPayload(postID), "create_media_link", mediaReferer(options), options)
}

func UpscaleVideo(ctx context.Context, token, videoID string, options MediaOptions) (map[string]any, error) {
	return postMediaWithProxy(ctx, protocol.VideoUpscaleURL, token, protocol.BuildVideoUpscalePayload(videoID), "upscale_video", mediaReferer(options), options)
}

func postMediaWithProxy(ctx context.Context, rawURL, token string, payload map[string]any, label, referer string, options MediaOptions) (map[string]any, error) {
	option := normalizeMediaOptions(options)
	lease, err := option.ProxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope: controlproxy.ProxyScopeApp,
		Kind:  controlproxy.RequestKindHTTP,
	})
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	result, err := option.Client.PostJSON(ctx, MediaHTTPRequest{
		URL:     rawURL,
		Token:   token,
		Payload: body,
		Lease:   &lease,
		Timeout: option.Timeout,
		Origin:  "https://grok.com",
		Referer: referer,
	})
	if err != nil {
		return nil, handleMediaError(ctx, option.ProxyRuntime, lease, label, err)
	}
	status := 200
	_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{
		Kind:       controlproxy.ProxyFeedbackSuccess,
		StatusCode: &status,
	})
	return result, nil
}

func normalizeMediaOptions(options MediaOptions) MediaOptions {
	if options.ProxyRuntime == nil {
		options.ProxyRuntime = missingMediaProxyRuntime{}
	}
	if options.Client == nil {
		options.Client = netMediaHTTPClient{}
	}
	if options.Timeout == 0 {
		options.Timeout = configuredMediaTimeout()
	}
	return options
}

func configuredMediaTimeout() time.Duration {
	seconds := mediaTimeoutProvider(defaultMediaTimeout.Seconds())
	if seconds <= 0 {
		return defaultMediaTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}

func mediaReferer(options MediaOptions) string {
	if options.Referer != "" {
		return options.Referer
	}
	return "https://grok.com"
}

func handleMediaError(ctx context.Context, runtime MediaProxyRuntime, lease controlproxy.ProxyLease, label string, err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		_ = runtime.Feedback(ctx, lease, UpstreamFeedback(upstream))
		return err
	}
	_ = runtime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
	return platform.NewUpstreamError(fmt.Sprintf("%s: transport error: %v", label, err), 502, err.Error())
}

type netMediaHTTPClient struct{}

func (netMediaHTTPClient) PostJSON(ctx context.Context, request MediaHTTPRequest) (map[string]any, error) {
	return PostJSON(ctx, request.URL, request.Token, request.Payload, HTTPOptions{
		Lease:   request.Lease,
		Timeout: request.Timeout,
		Origin:  request.Origin,
		Referer: request.Referer,
	})
}

type missingMediaProxyRuntime struct{}

func (missingMediaProxyRuntime) Acquire(context.Context, ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return controlproxy.ProxyLease{}, fmt.Errorf("media proxy runtime is not configured")
}

func (missingMediaProxyRuntime) Feedback(context.Context, controlproxy.ProxyLease, controlproxy.ProxyFeedback) error {
	return nil
}
