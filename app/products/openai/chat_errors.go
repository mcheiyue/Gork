package openai

import (
	"context"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
)

func upstreamBodyExcerpt(err *platform.UpstreamError, limit int) string {
	if err == nil {
		return "-"
	}
	if limit <= 0 {
		limit = 240
	}
	body := strings.ReplaceAll(err.Body, "\n", `\n`)
	if len(body) > limit {
		body = body[:limit]
	}
	if body == "" {
		return "-"
	}
	return body
}

func transportUpstreamError(err error, context string) *platform.UpstreamError {
	if err == nil {
		return nil
	}
	var upstream *platform.UpstreamError
	if errorsAs(err, &upstream) {
		return upstream
	}
	body := strings.ReplaceAll(err.Error(), "\n", `\n`)
	if len(body) > 400 {
		body = body[:400]
	}
	return platform.NewUpstreamError(context+": "+err.Error(), 502, body)
}

func logTaskException(err error) {
	_ = err
}

func quotaSync(ctx context.Context, token string, modeID int) {
	if currentAccountStrategy() != "quota" {
		return
	}
	service := chatRefreshService()
	if service == nil {
		return
	}
	_ = service.RefreshCall(ctx, token, modeID)
}

func failSync(ctx context.Context, token string, modeID int, err error) {
	service := chatRefreshService()
	if service == nil {
		return
	}
	_ = service.RecordFailure(ctx, token, modeID, err)
	if currentAccountStrategy() == "quota" && upstreamStatus(err) == 429 {
		_, _ = service.RefreshOnDemand(ctx)
	}
}

func shouldRetryUpstream(err error, retryCodes map[int]struct{}) bool {
	if _, ok := retryCodes[upstreamStatus(err)]; ok {
		return true
	}
	return isInvalidCredentials(err)
}

func feedbackKind(err error) accountFeedbackKind {
	if err == nil {
		return feedbackKindServerError
	}
	if isInvalidCredentials(err) {
		return feedbackKindUnauthorized
	}
	switch upstreamStatus(err) {
	case 429:
		return feedbackKindRateLimited
	case 401:
		return feedbackKindUnauthorized
	case 403:
		return feedbackKindForbidden
	default:
		return feedbackKindServerError
	}
}
