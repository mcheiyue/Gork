package account

import appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"

type StatePolicy struct {
	FailThreshold    int
	ForbiddenStrikes int
	DefaultCoolingMS int64
}

var defaultStatePolicy = StatePolicy{
	FailThreshold:    5,
	ForbiddenStrikes: 1,
	DefaultCoolingMS: 15 * 60 * 1000,
}

var stateMachineNowMS = appruntime.NowMS

type AccountFeedback struct {
	Kind           FeedbackKind
	ModeID         int
	At             int64
	StatusCode     *int
	Reason         string
	QuotaWindow    *QuotaWindow
	RetryAfterMS   *int64
	ConfirmExpired bool
	ApplyUsage     bool
}

type FeedbackFromStatusOptions struct {
	Reason         string
	RetryAfterMS   *int64
	ConfirmExpired bool
}

func AccountFeedbackFromStatusCode(statusCode int, modeID int, options FeedbackFromStatusOptions) AccountFeedback {
	status := statusCode
	return AccountFeedback{
		Kind:           feedbackKindFromStatus(statusCode),
		ModeID:         modeID,
		At:             stateMachineNowMS(),
		StatusCode:     &status,
		Reason:         options.Reason,
		RetryAfterMS:   cloneInt64Ptr(options.RetryAfterMS),
		ConfirmExpired: options.ConfirmExpired,
		ApplyUsage:     false,
	}
}

func feedbackKindFromStatus(statusCode int) FeedbackKind {
	switch {
	case statusCode == 401:
		return FeedbackKindUnauthorized
	case statusCode == 403:
		return FeedbackKindForbidden
	case statusCode == 429:
		return FeedbackKindRateLimited
	case statusCode >= 500:
		return FeedbackKindServerError
	case statusCode >= 200 && statusCode < 300:
		return FeedbackKindSuccess
	default:
		return FeedbackKindServerError
	}
}

func normalizeStatePolicy(policy StatePolicy) StatePolicy {
	if policy.FailThreshold == 0 {
		policy.FailThreshold = defaultStatePolicy.FailThreshold
	}
	if policy.ForbiddenStrikes == 0 {
		policy.ForbiddenStrikes = defaultStatePolicy.ForbiddenStrikes
	}
	if policy.DefaultCoolingMS == 0 {
		policy.DefaultCoolingMS = defaultStatePolicy.DefaultCoolingMS
	}
	return policy
}
