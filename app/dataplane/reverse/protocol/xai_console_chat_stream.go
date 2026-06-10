package protocol

import (
	"context"
	"fmt"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

type ConsoleProxy interface {
	Acquire(ctx context.Context) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type ConsoleStreamPoster interface {
	PostConsoleStream(ctx context.Context, request ConsoleStreamRequest) (ConsoleStreamResponse, error)
}

type ConsoleStreamRequest struct {
	Token    string
	Payload  map[string]any
	TimeoutS float64
	Lease    controlproxy.ProxyLease
}

type ConsoleStreamResponse struct {
	StatusCode int
	Body       string
	Lines      []string
}

type ConsoleStreamOptions struct {
	Proxy    ConsoleProxy
	Poster   ConsoleStreamPoster
	TimeoutS float64
}

type ConsoleStreamEvent struct {
	EventType string
	Data      string
}

func StreamConsoleChat(ctx context.Context, token string, payload map[string]any, options ConsoleStreamOptions) ([]ConsoleStreamEvent, error) {
	timeout := options.TimeoutS
	if timeout == 0 {
		timeout = 120.0
	}
	proxy := options.Proxy
	if proxy == nil {
		proxy = noopConsoleProxy{}
	}
	poster := options.Poster
	if poster == nil {
		poster = missingConsolePoster{}
	}
	lease, err := proxy.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	response, err := poster.PostConsoleStream(ctx, ConsoleStreamRequest{Token: token, Payload: payload, TimeoutS: timeout, Lease: lease})
	if err != nil {
		_ = proxy.Feedback(ctx, lease, ConsoleTransportErrorFeedback())
		return nil, platform.NewUpstreamError(fmt.Sprintf("Console transport failed: %v", err), 502, "")
	}
	if response.StatusCode != 200 {
		body := response.Body
		if len(body) > 400 {
			body = body[:400]
		}
		_ = proxy.Feedback(ctx, lease, ConsoleStatusFeedback(response.StatusCode))
		return nil, platform.NewUpstreamError(fmt.Sprintf("Console API returned %d", response.StatusCode), response.StatusCode, body)
	}
	_ = proxy.Feedback(ctx, lease, ConsoleSuccessFeedback())
	currentEvent := ""
	events := []ConsoleStreamEvent{}
	for _, rawLine := range response.Lines {
		kind, value := ClassifyConsoleLine(rawLine)
		switch kind {
		case "event":
			currentEvent = value
		case "data":
			events = append(events, ConsoleStreamEvent{EventType: currentEvent, Data: value})
			currentEvent = ""
		case "done":
			return events, nil
		}
	}
	return events, nil
}

type noopConsoleProxy struct{}

func (noopConsoleProxy) Acquire(context.Context) (controlproxy.ProxyLease, error) {
	return controlproxy.NewProxyLease(""), nil
}

func (noopConsoleProxy) Feedback(context.Context, controlproxy.ProxyLease, controlproxy.ProxyFeedback) error {
	return nil
}

type missingConsolePoster struct{}

func (missingConsolePoster) PostConsoleStream(context.Context, ConsoleStreamRequest) (ConsoleStreamResponse, error) {
	return ConsoleStreamResponse{}, platform.NewUpstreamError("console stream poster is not configured", 502, "")
}
