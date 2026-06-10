package reverse

import (
	"fmt"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

type AccountPatch struct {
	Token          string
	Pool           *string
	Status         any
	Tags           []string
	AddTags        []string
	RemoveTags     []string
	QuotaAuto      map[string]any
	QuotaFast      map[string]any
	QuotaExpert    map[string]any
	QuotaHeavy     map[string]any
	QuotaGrok43    map[string]any
	QuotaConsole   map[string]any
	UsageUseDelta  *int
	UsageFailDelta *int
	UsageSyncDelta *int
	LastUseAt      *int64
	LastFailAt     *int64
	LastFailReason *string
	LastSyncAt     *int64
	LastClearAt    *int64
	StateReason    *string
	ExtMerge       map[string]any
	ClearFailures  bool
}

type AccountFeedbackOptions struct {
	ModeID int
	Clock  func() int64
}

func BuildAccountFeedback(token string, result ReverseResult, options ...AccountFeedbackOptions) AccountPatch {
	option := accountFeedbackOptions(options...)
	ts := option.Clock()
	if result.Category == ResultCategorySuccess {
		return AccountPatch{
			Token:         token,
			UsageUseDelta: intPtr(1),
			LastUseAt:     int64Ptr(ts),
		}
	}
	if result.Category == ResultCategoryRateLimited {
		patch := AccountPatch{
			Token:          token,
			UsageFailDelta: intPtr(1),
			LastFailAt:     int64Ptr(ts),
			LastFailReason: stringPtr(fmt.Sprintf("rate_limited (mode=%d)", option.ModeID)),
		}
		switch option.ModeID {
		case 0:
			patch.QuotaAuto = map[string]any{"remaining": 0}
		case 1:
			patch.QuotaFast = map[string]any{"remaining": 0}
		case 2:
			patch.QuotaExpert = map[string]any{"remaining": 0}
		}
		return patch
	}
	reason := result.Error
	if reason == "" {
		reason = result.Category.Name()
	}
	return AccountPatch{
		Token:          token,
		UsageFailDelta: intPtr(1),
		LastFailAt:     int64Ptr(ts),
		LastFailReason: stringPtr(reason),
	}
}

func BuildProxyFeedback(result ReverseResult) controlproxy.ProxyFeedback {
	statusCode := result.StatusCode
	return controlproxy.ProxyFeedback{
		Kind:       proxyFeedbackKindForCategory(result.Category),
		StatusCode: &statusCode,
		Reason:     result.Error,
	}
}

func accountFeedbackOptions(options ...AccountFeedbackOptions) AccountFeedbackOptions {
	option := AccountFeedbackOptions{Clock: platformruntime.NowMS}
	if len(options) > 0 {
		option = options[0]
		if option.Clock == nil {
			option.Clock = platformruntime.NowMS
		}
	}
	return option
}

func proxyFeedbackKindForCategory(category ResultCategory) controlproxy.ProxyFeedbackKind {
	switch category {
	case ResultCategorySuccess:
		return controlproxy.ProxyFeedbackSuccess
	case ResultCategoryRateLimited:
		return controlproxy.ProxyFeedbackRateLimited
	case ResultCategoryAuthFailure:
		return controlproxy.ProxyFeedbackUnauthorized
	case ResultCategoryForbidden:
		return controlproxy.ProxyFeedbackChallenge
	case ResultCategoryUpstream5xx:
		return controlproxy.ProxyFeedbackUpstream5xx
	case ResultCategoryTransportErr:
		return controlproxy.ProxyFeedbackTransportError
	default:
		return controlproxy.ProxyFeedbackTransportError
	}
}

func intPtr(value int) *int {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
