package transport

import (
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestUpstreamFeedbackMapsStatusLikePython(t *testing.T) {
	tests := []struct {
		status int
		want   controlproxy.ProxyFeedbackKind
	}{
		{status: 401, want: controlproxy.ProxyFeedbackUnauthorized},
		{status: 403, want: controlproxy.ProxyFeedbackChallenge},
		{status: 429, want: controlproxy.ProxyFeedbackRateLimited},
		{status: 500, want: controlproxy.ProxyFeedbackUpstream5xx},
		{status: 503, want: controlproxy.ProxyFeedbackUpstream5xx},
		{status: 418, want: controlproxy.ProxyFeedbackTransportError},
	}

	for _, tt := range tests {
		err := platform.NewUpstreamError("upstream failed", tt.status, "body")
		got := UpstreamFeedback(err)
		if got.Kind != tt.want {
			t.Fatalf("status %d kind = %v, want %v", tt.status, got.Kind, tt.want)
		}
		if got.StatusCode == nil || *got.StatusCode != tt.status {
			t.Fatalf("status %d feedback status = %#v", tt.status, got.StatusCode)
		}
	}
}

func TestUpstreamFeedbackOmitsZeroStatusLikePythonNone(t *testing.T) {
	err := &platform.UpstreamError{AppError: &platform.AppError{Status: 0}}
	got := UpstreamFeedback(err)

	if got.Kind != controlproxy.ProxyFeedbackTransportError {
		t.Fatalf("zero status kind = %v, want transport_error", got.Kind)
	}
	if got.StatusCode != nil {
		t.Fatalf("zero status feedback status = %#v, want nil", got.StatusCode)
	}
}
