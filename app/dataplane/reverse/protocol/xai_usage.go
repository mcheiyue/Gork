package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

const UsageQuotaSourceReal = "real"

var usageModeNames = map[int]string{
	0: "auto",
	1: "fast",
	2: "expert",
	3: "heavy",
	4: "grok-420-computer-use-sa",
}

var defaultUsageWindowSeconds = map[int]int{
	0: 7200,
	1: 86400,
	2: 7200,
	3: 7200,
	4: 7200,
}

type RateLimitQuota struct {
	Remaining     int
	Total         int
	WindowSeconds int
}

type UsageQuotaWindow struct {
	Remaining     int
	Total         int
	WindowSeconds int
	ResetAt       int64
	SyncedAt      int64
	Source        string
}

type UsageFetcher interface {
	FetchUsage(ctx context.Context, token, modeName string) (map[string]any, error)
}

type UsageFetchOptions struct {
	Fetcher  UsageFetcher
	SyncedAt int64
}

func BuildUsagePayload(modeName string) []byte {
	body, _ := json.Marshal(map[string]string{"modelName": modeName})
	return body
}

func ParseRateLimits(body map[string]any, defaultWindowSeconds int) *RateLimitQuota {
	remaining, ok := usageNumberAsInt(body["remainingQueries"])
	if !ok {
		return nil
	}
	total, ok := usageNumberAsInt(body["totalQueries"])
	if !ok {
		total = remaining
	}
	windowSeconds := usageWindowSeconds(body["windowSizeSeconds"], defaultWindowSeconds)
	return &RateLimitQuota{Remaining: remaining, Total: total, WindowSeconds: windowSeconds}
}

func usageNumberAsInt(value any) (int, bool) {
	if i, ok := numberAsInt(value); ok {
		return i, true
	}
	if s, ok := value.(string); ok {
		if strings.TrimSpace(s) == "" {
			return 0, false
		}
		i, err := strconv.Atoi(strings.TrimSpace(s))
		return i, err == nil
	}
	return 0, false
}

func usageWindowSeconds(value any, defaultWindowSeconds int) int {
	if s, ok := value.(string); ok {
		if s == "" {
			return defaultWindowSeconds
		}
		if i, parsed := usageNumberAsInt(s); parsed {
			return i
		}
		return defaultWindowSeconds
	}
	i, ok := usageNumberAsInt(value)
	if !ok || i == 0 {
		return defaultWindowSeconds
	}
	return i
}

func ToUsageQuotaWindow(data RateLimitQuota, syncedAt int64) UsageQuotaWindow {
	return UsageQuotaWindow{
		Remaining:     data.Remaining,
		Total:         data.Total,
		WindowSeconds: data.WindowSeconds,
		ResetAt:       syncedAt + int64(data.WindowSeconds)*1000,
		SyncedAt:      syncedAt,
		Source:        UsageQuotaSourceReal,
	}
}

func FetchAllUsageQuotas(ctx context.Context, token string, modeIDs []int, options UsageFetchOptions) (map[int]UsageQuotaWindow, error) {
	if len(modeIDs) == 0 {
		modeIDs = []int{0, 1, 2, 3, 4}
	}
	fetcher := options.Fetcher
	if fetcher == nil {
		fetcher = missingUsageFetcher{}
	}
	windows := map[int]UsageQuotaWindow{}
	for _, modeID := range modeIDs {
		window, err := FetchModeUsageQuota(ctx, token, modeID, UsageFetchOptions{Fetcher: fetcher, SyncedAt: options.SyncedAt})
		if err != nil {
			if IsInvalidCredentialsError(err) {
				return nil, err
			}
			continue
		}
		if window != nil {
			windows[modeID] = *window
		}
	}
	if len(windows) == 0 {
		return nil, nil
	}
	return windows, nil
}

func FetchModeUsageQuota(ctx context.Context, token string, modeID int, options UsageFetchOptions) (*UsageQuotaWindow, error) {
	fetcher := options.Fetcher
	if fetcher == nil {
		fetcher = missingUsageFetcher{}
	}
	modeName := usageModeNames[modeID]
	if modeName == "" {
		modeName = "auto"
	}
	body, err := fetcher.FetchUsage(ctx, token, modeName)
	if err != nil {
		if IsInvalidCredentialsError(err) {
			return nil, err
		}
		return nil, nil
	}
	defaultWindow := defaultUsageWindowSeconds[modeID]
	if defaultWindow == 0 {
		defaultWindow = 72000
	}
	quota := ParseRateLimits(body, defaultWindow)
	if quota == nil {
		return nil, nil
	}
	window := ToUsageQuotaWindow(*quota, options.SyncedAt)
	return &window, nil
}

func IsInvalidCredentialsBody(body string) bool {
	text := strings.ToLower(body)
	return strings.Contains(text, "invalid-credentials") ||
		strings.Contains(text, "bad-credentials") ||
		strings.Contains(text, "failed to look up session id") ||
		strings.Contains(text, "blocked-user") ||
		strings.Contains(text, "email-domain-rejected") ||
		strings.Contains(text, "session not found") ||
		strings.Contains(text, "account suspended") ||
		strings.Contains(text, "token revoked") ||
		strings.Contains(text, "token expired")
}

func IsInvalidCredentialsError(err error) bool {
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		return false
	}
	if upstream.Status != 400 && upstream.Status != 401 && upstream.Status != 403 {
		return false
	}
	return IsInvalidCredentialsBody(upstream.Body)
}

func UsageProxyFeedbackKindForError(err error, status int) controlproxy.ProxyFeedbackKind {
	if IsInvalidCredentialsError(err) {
		return controlproxy.ProxyFeedbackForbidden
	}
	if status == 429 {
		return controlproxy.ProxyFeedbackRateLimited
	}
	if status == 403 {
		return controlproxy.ProxyFeedbackChallenge
	}
	if status == 401 {
		return controlproxy.ProxyFeedbackUnauthorized
	}
	if status >= 500 {
		return controlproxy.ProxyFeedbackUpstream5xx
	}
	return controlproxy.ProxyFeedbackTransportError
}

type missingUsageFetcher struct{}

func (missingUsageFetcher) FetchUsage(context.Context, string, string) (map[string]any, error) {
	return nil, platform.NewUpstreamError("usage fetcher is not configured", 502, "")
}
