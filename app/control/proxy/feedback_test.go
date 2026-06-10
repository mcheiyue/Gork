package proxy

import "testing"

func TestClassifyStatusCodeMatchesPython(t *testing.T) {
	cases := []struct {
		status int
		want   ProxyFeedbackKind
	}{
		{200, ProxyFeedbackSuccess},
		{401, ProxyFeedbackUnauthorized},
		{403, ProxyFeedbackChallenge},
		{429, ProxyFeedbackRateLimited},
		{500, ProxyFeedbackUpstream5xx},
		{503, ProxyFeedbackUpstream5xx},
		{404, ProxyFeedbackForbidden},
		{499, ProxyFeedbackForbidden},
	}

	for _, tt := range cases {
		if got := ClassifyStatusCode(tt.status); got != tt.want {
			t.Fatalf("ClassifyStatusCode(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestBuildFeedbackMatchesPython(t *testing.T) {
	feedback := BuildFeedback(200)
	if feedback.Kind != ProxyFeedbackSuccess || feedback.StatusCode == nil || *feedback.StatusCode != 200 ||
		feedback.Reason != "" || feedback.RetryAfterMS != nil {
		t.Fatalf("BuildFeedback default = %#v", feedback)
	}

	retryAfter := int64(1500)
	rateLimited := BuildFeedback(429, BuildFeedbackOptions{
		Reason:       "slow down",
		RetryAfterMS: &retryAfter,
	})
	if rateLimited.Kind != ProxyFeedbackRateLimited || rateLimited.StatusCode == nil ||
		*rateLimited.StatusCode != 429 || rateLimited.Reason != "slow down" ||
		rateLimited.RetryAfterMS == nil || *rateLimited.RetryAfterMS != retryAfter {
		t.Fatalf("BuildFeedback rate limited = %#v", rateLimited)
	}

	challenge := BuildFeedback(403, BuildFeedbackOptions{IsCloudflare: true})
	if challenge.Kind != ProxyFeedbackChallenge {
		t.Fatalf("cloudflare 403 kind = %q, want %q", challenge.Kind, ProxyFeedbackChallenge)
	}
}
