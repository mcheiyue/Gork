package transport

import (
	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func UpstreamFeedback(exc *platform.UpstreamError) controlproxy.ProxyFeedback {
	status := 0
	if exc != nil && exc.AppError != nil {
		status = exc.Status
	}
	feedback := controlproxy.NewProxyFeedback(proxyFeedbackKindForStatus(status))
	if status != 0 {
		feedback.StatusCode = &status
	}
	return feedback
}

func proxyFeedbackKindForStatus(status int) controlproxy.ProxyFeedbackKind {
	switch {
	case status == 401:
		return controlproxy.ProxyFeedbackUnauthorized
	case status == 403:
		return controlproxy.ProxyFeedbackChallenge
	case status == 429:
		return controlproxy.ProxyFeedbackRateLimited
	case status >= 500:
		return controlproxy.ProxyFeedbackUpstream5xx
	default:
		return controlproxy.ProxyFeedbackTransportError
	}
}
