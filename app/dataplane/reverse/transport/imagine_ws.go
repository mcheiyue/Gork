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
	defaultImagineTimeout        = 120 * time.Second
	defaultImagineStreamTimeout  = 10 * time.Second
	defaultImagineInterRoundWait = 2 * time.Second
)

var (
	imagineTimeoutProvider       = func() float64 { return config.GlobalConfig.GetFloat("image.timeout", 120.0) }
	imagineStreamTimeoutProvider = func() float64 { return config.GlobalConfig.GetFloat("image.stream_timeout", 10.0) }
)

type ImagineProxyRuntime interface {
	Acquire(ctx context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type ImagineWebSocketClient interface {
	Connect(ctx context.Context, request ImagineWebSocketConnectRequest) (ImagineWebSocketConnection, error)
}

type ImagineWebSocketConnection interface {
	SendJSON(ctx context.Context, payload map[string]any) error
	Receive(ctx context.Context, timeout time.Duration) (ImagineWebSocketMessage, error)
	Close() error
}

type ImagineWebSocketMessageType string

const (
	ImagineWebSocketTextMessage   ImagineWebSocketMessageType = "text"
	ImagineWebSocketClosedMessage ImagineWebSocketMessageType = "closed"
	ImagineWebSocketErrorMessage  ImagineWebSocketMessageType = "error"
)

type ImagineWebSocketMessage struct {
	Type ImagineWebSocketMessageType
	Data string
}

type ImagineWebSocketConnectRequest struct {
	URL       string
	Headers   map[string]string
	Timeout   time.Duration
	WSOptions ImagineWebSocketOptions
	Lease     controlproxy.ProxyLease
}

type ImagineWebSocketOptions struct {
	Heartbeat      time.Duration
	ReceiveTimeout time.Duration
}

type ImagineOptions struct {
	ProxyRuntime   ImagineProxyRuntime
	Client         ImagineWebSocketClient
	Timeout        time.Duration
	StreamTimeout  time.Duration
	InterRoundWait time.Duration
	AspectRatio    string
	N              int
	EnableNSFW     *bool
	EnablePro      bool
	Now            func() time.Time
}

func StreamImages(ctx context.Context, token, prompt string, options ImagineOptions) ([]map[string]any, error) {
	option := normalizeImagineOptions(options)
	events := []map[string]any{}
	collected := 0
	for collected < option.N {
		lease, err := option.ProxyRuntime.Acquire(ctx, controlproxy.AcquireOptions{
			Scope: controlproxy.ProxyScopeApp,
			Kind:  controlproxy.RequestKindWebSocket,
		})
		if err != nil {
			return nil, err
		}
		conn, err := option.Client.Connect(ctx, imagineConnectRequest(token, lease, option))
		if err != nil {
			_ = option.ProxyRuntime.Feedback(ctx, lease, imagineConnectFeedback(err))
			return append(events, imagineConnectErrorEvent(err)), nil
		}

		wsClosed, err := runImagineConnection(ctx, conn, prompt, option, &events, &collected)
		_ = conn.Close()
		if err != nil {
			_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
			return append(events, map[string]any{"type": "error", "error_code": "connection_failed", "error": err.Error()}), nil
		}
		if imagineLastEventIsError(events) {
			_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError})
			return events, nil
		}

		status := 200
		_ = option.ProxyRuntime.Feedback(ctx, lease, controlproxy.ProxyFeedback{
			Kind:       controlproxy.ProxyFeedbackSuccess,
			StatusCode: &status,
		})
		if collected >= option.N {
			return events, nil
		}
		if !wsClosed {
			continue
		}
	}
	return events, nil
}

func runImagineConnection(ctx context.Context, conn ImagineWebSocketConnection, prompt string, option ImagineOptions, events *[]map[string]any, collected *int) (bool, error) {
	wsClosed := false
	for *collected < option.N {
		result, err := streamImagineRound(ctx, conn, imagineRoundOptions{
			Prompt:         prompt,
			AspectRatio:    option.AspectRatio,
			EnableNSFW:     option.enableNSFW(),
			EnablePro:      option.EnablePro,
			Needed:         option.N - *collected,
			StreamTimeout:  option.StreamTimeout,
			RoundTimeout:   option.Timeout,
			InterRoundWait: option.InterRoundWait,
			Now:            option.Now,
		})
		if err != nil {
			return false, err
		}
		for _, event := range result.Events {
			if event["is_final"] == true {
				*collected = *collected + 1
			}
			*events = append(*events, event)
			if event["type"] == "error" {
				return result.WSClosed, nil
			}
		}
		wsClosed = result.WSClosed
		if wsClosed || *collected >= option.N {
			break
		}
	}
	return wsClosed, nil
}

func normalizeImagineOptions(options ImagineOptions) ImagineOptions {
	if options.ProxyRuntime == nil {
		options.ProxyRuntime = missingImagineProxyRuntime{}
	}
	if options.Client == nil {
		options.Client = missingImagineWebSocketClient{}
	}
	if options.Timeout == 0 {
		options.Timeout = configuredImagineTimeout()
	}
	if options.StreamTimeout == 0 {
		options.StreamTimeout = configuredImagineStreamTimeout()
	}
	if options.InterRoundWait == 0 {
		options.InterRoundWait = defaultImagineInterRoundWait
	}
	if options.AspectRatio == "" {
		options.AspectRatio = "2:3"
	}
	if options.N <= 0 {
		options.N = 1
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func configuredImagineTimeout() time.Duration {
	return configuredImagineDuration(imagineTimeoutProvider(), defaultImagineTimeout)
}

func configuredImagineStreamTimeout() time.Duration {
	return configuredImagineDuration(imagineStreamTimeoutProvider(), defaultImagineStreamTimeout)
}

func configuredImagineDuration(seconds float64, fallback time.Duration) time.Duration {
	if seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds * float64(time.Second))
}

func (options ImagineOptions) enableNSFW() bool {
	if options.EnableNSFW == nil {
		return true
	}
	return *options.EnableNSFW
}

func imagineConnectRequest(token string, lease controlproxy.ProxyLease, option ImagineOptions) ImagineWebSocketConnectRequest {
	return ImagineWebSocketConnectRequest{
		URL:     protocol.WSImagineURL,
		Headers: adapters.BuildWSHeaders(token, adapters.WSHeaderOptions{Lease: &lease}),
		Timeout: option.Timeout,
		WSOptions: ImagineWebSocketOptions{
			Heartbeat:      20 * time.Second,
			ReceiveTimeout: option.StreamTimeout,
		},
		Lease: lease,
	}
}

func imagineConnectFeedback(err error) controlproxy.ProxyFeedback {
	status := imagineStatusFromError(err)
	if status == 0 {
		return controlproxy.ProxyFeedback{Kind: controlproxy.ProxyFeedbackTransportError}
	}
	return UpstreamFeedback(platform.NewUpstreamError("connect failed", status, ""))
}

func imagineConnectErrorEvent(err error) map[string]any {
	code := "connection_failed"
	if imagineStatusFromError(err) == 429 {
		code = "rate_limit_exceeded"
	}
	return map[string]any{"type": "error", "error_code": code, "error": err.Error()}
}

func imagineStatusFromError(err error) int {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) && upstream.AppError != nil {
		return upstream.Status
	}
	return 0
}

func imagineLastEventIsError(events []map[string]any) bool {
	return len(events) > 0 && events[len(events)-1]["type"] == "error"
}

type missingImagineProxyRuntime struct{}

func (missingImagineProxyRuntime) Acquire(context.Context, ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return controlproxy.ProxyLease{}, fmt.Errorf("imagine proxy runtime is not configured")
}

func (missingImagineProxyRuntime) Feedback(context.Context, controlproxy.ProxyLease, controlproxy.ProxyFeedback) error {
	return nil
}

type missingImagineWebSocketClient struct{}

func (missingImagineWebSocketClient) Connect(context.Context, ImagineWebSocketConnectRequest) (ImagineWebSocketConnection, error) {
	return nil, platform.NewUpstreamError("imagine websocket client is not configured", 502, "")
}
