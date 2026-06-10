package proxy

type BuildFeedbackOptions struct {
	IsCloudflare bool
	Reason       string
	RetryAfterMS *int64
}

func ClassifyStatusCode(statusCode int) ProxyFeedbackKind {
	if statusCode == 200 {
		return ProxyFeedbackSuccess
	}
	if statusCode == 401 {
		return ProxyFeedbackUnauthorized
	}
	if statusCode == 403 {
		return ProxyFeedbackChallenge
	}
	if statusCode == 429 {
		return ProxyFeedbackRateLimited
	}
	if statusCode >= 500 {
		return ProxyFeedbackUpstream5xx
	}
	return ProxyFeedbackForbidden
}

func BuildFeedback(statusCode int, options ...BuildFeedbackOptions) ProxyFeedback {
	opts := BuildFeedbackOptions{}
	if len(options) > 0 {
		opts = options[0]
	}

	kind := ClassifyStatusCode(statusCode)
	if opts.IsCloudflare && statusCode == 403 {
		kind = ProxyFeedbackChallenge
	}

	return ProxyFeedback{
		Kind:         kind,
		StatusCode:   &statusCode,
		Reason:       opts.Reason,
		RetryAfterMS: opts.RetryAfterMS,
	}
}
