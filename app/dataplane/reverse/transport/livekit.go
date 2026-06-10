package transport

import (
	"context"
	"errors"
	"fmt"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/proxy/adapters"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

const (
	defaultLiveKitTokenTimeout = 60 * time.Second
	defaultLiveKitWSTimeout    = 120 * time.Second
)

var liveKitTimeoutProvider = func(defaultSeconds float64) float64 {
	return config.GlobalConfig.GetFloat("voice.timeout", defaultSeconds)
}

type LiveKitProxyRuntime interface {
	Acquire(ctx context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type LiveKitHTTPClient interface {
	PostJSON(ctx context.Context, request LiveKitHTTPRequest) (map[string]any, error)
}

type LiveKitHTTPRequest struct {
	URL     string
	Token   string
	Payload []byte
	Lease   *controlproxy.ProxyLease
	Timeout time.Duration
	Origin  string
	Referer string
}

type LiveKitOptions struct {
	ProxyRuntime      LiveKitProxyRuntime
	Client            LiveKitHTTPClient
	Timeout           time.Duration
	Voice             string
	Personality       string
	Speed             float64
	CustomInstruction string
}

type LiveKitWebSocketClient interface {
	Connect(ctx context.Context, request LiveKitWebSocketRequest) (LiveKitWebSocketConnection, error)
}

type LiveKitWebSocketConnection interface {
	Close() error
}

type LiveKitWebSocketRequest struct {
	URL     string
	Headers map[string]string
	Timeout time.Duration
	Lease   controlproxy.ProxyLease
	OnClose func(context.Context) error
}

type LiveKitWSOptions struct {
	ProxyRuntime LiveKitProxyRuntime
	Client       LiveKitWebSocketClient
	Timeout      time.Duration
}

func FetchLiveKitToken(ctx context.Context, token string, options LiveKitOptions) (map[string]any, error) {
	option := normalizeLiveKitOptions(options)
	lease, err := option.ProxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope: controlproxy.ProxyScopeApp,
		Kind:  controlproxy.RequestKindHTTP,
	})
	if err != nil {
		return nil, err
	}

	result, err := option.Client.PostJSON(ctx, LiveKitHTTPRequest{
		URL:     protocol.LiveKitTokenURL,
		Token:   token,
		Payload: liveKitTokenPayload(option),
		Lease:   &lease,
		Timeout: option.Timeout,
		Origin:  "https://grok.com",
		Referer: "https://grok.com/",
	})
	if err != nil {
		return nil, handleLiveKitTokenError(ctx, option.ProxyRuntime, lease, err)
	}
	status := 200
	_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{
		Kind:       controlproxy.ProxyFeedbackSuccess,
		StatusCode: &status,
	})
	return result, nil
}

func ConnectLiveKitWS(ctx context.Context, token, accessToken string, options LiveKitWSOptions) (LiveKitWebSocketConnection, error) {
	option := normalizeLiveKitWSOptions(options)
	lease, err := option.ProxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
		Scope: controlproxy.ProxyScopeApp,
		Kind:  controlproxy.RequestKindWebSocket,
	})
	if err != nil {
		return nil, err
	}
	request := LiveKitWebSocketRequest{
		URL:     protocol.BuildLiveKitWSURL(accessToken),
		Headers: adapters.BuildWSHeaders(token, adapters.WSHeaderOptions{Lease: &lease}),
		Timeout: option.Timeout,
		Lease:   lease,
		OnClose: func(closeCtx context.Context) error {
			status := 200
			_ = option.ProxyRuntime.Feedback(closeCtx, lease, controlproxy.ProxyFeedback{
				Kind:       controlproxy.ProxyFeedbackSuccess,
				StatusCode: &status,
			})
			return nil
		},
	}
	connection, err := option.Client.Connect(ctx, request)
	if err != nil {
		_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
		return nil, platform.NewUpstreamError(fmt.Sprintf("connect_livekit_ws: %v", err), 502, err.Error())
	}
	return connection, nil
}

func normalizeLiveKitOptions(options LiveKitOptions) LiveKitOptions {
	if options.ProxyRuntime == nil {
		options.ProxyRuntime = missingLiveKitProxyRuntime{}
	}
	if options.Client == nil {
		options.Client = netLiveKitHTTPClient{}
	}
	if options.Timeout == 0 {
		options.Timeout = configuredLiveKitTimeout(defaultLiveKitTokenTimeout)
	}
	return options
}

func normalizeLiveKitWSOptions(options LiveKitWSOptions) LiveKitWSOptions {
	if options.ProxyRuntime == nil {
		options.ProxyRuntime = missingLiveKitProxyRuntime{}
	}
	if options.Client == nil {
		options.Client = missingLiveKitWebSocketClient{}
	}
	if options.Timeout == 0 {
		options.Timeout = configuredLiveKitTimeout(defaultLiveKitWSTimeout)
	}
	return options
}

func configuredLiveKitTimeout(fallback time.Duration) time.Duration {
	seconds := liveKitTimeoutProvider(fallback.Seconds())
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}

func liveKitTokenPayload(options LiveKitOptions) []byte {
	return protocol.BuildLiveKitTokenRequestPayload(protocol.LiveKitTokenOptions{
		Voice:             options.Voice,
		Personality:       options.Personality,
		Speed:             options.Speed,
		CustomInstruction: options.CustomInstruction,
	})
}

func handleLiveKitTokenError(ctx context.Context, runtime LiveKitProxyRuntime, lease controlproxy.ProxyLease, err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		_ = runtime.Feedback(ctx, lease, UpstreamFeedback(upstream))
		return err
	}
	_ = runtime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
	return platform.NewUpstreamError(fmt.Sprintf("fetch_livekit_token: transport error: %v", err), 502, err.Error())
}

type netLiveKitHTTPClient struct{}

func (netLiveKitHTTPClient) PostJSON(ctx context.Context, request LiveKitHTTPRequest) (map[string]any, error) {
	return PostJSON(ctx, request.URL, request.Token, request.Payload, HTTPOptions{
		Lease:   request.Lease,
		Timeout: request.Timeout,
		Origin:  request.Origin,
		Referer: request.Referer,
	})
}

type missingLiveKitProxyRuntime struct{}

func (missingLiveKitProxyRuntime) Acquire(context.Context, ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return controlproxy.ProxyLease{}, fmt.Errorf("livekit proxy runtime is not configured")
}

func (missingLiveKitProxyRuntime) Feedback(context.Context, controlproxy.ProxyLease, controlproxy.ProxyFeedback) error {
	return nil
}

type missingLiveKitWebSocketClient struct{}

func (missingLiveKitWebSocketClient) Connect(context.Context, LiveKitWebSocketRequest) (LiveKitWebSocketConnection, error) {
	return nil, platform.NewUpstreamError("livekit websocket client is not configured", 502, "")
}
