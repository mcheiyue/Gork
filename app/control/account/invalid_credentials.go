package account

import (
	"context"
	"errors"
	"time"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/logging"
)

const invalidCredentialsReason = "invalid_credentials"

var invalidCredentialsNowMS = func() int64 {
	return time.Now().UnixMilli()
}

type InvalidCredentialRepository interface {
	GetAccounts(context.Context, []string) ([]AccountRecord, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error)
}

func MarkAccountInvalidCredentials(
	ctx context.Context,
	repo InvalidCredentialRepository,
	token string,
	err error,
	source string,
) (bool, error) {
	if !protocol.IsInvalidCredentialsError(err) {
		return false, nil
	}
	records, getErr := repo.GetAccounts(ctx, []string{token})
	if getErr != nil {
		return false, getErr
	}
	ts := invalidCredentialsNowMS()
	status := AccountStatusExpired
	reason := invalidCredentialsReason
	patch := AccountPatch{
		Token:          token,
		Status:         &status,
		LastFailAt:     &ts,
		LastFailReason: &reason,
		StateReason:    &reason,
		ExtMerge:       invalidCredentialsExt(records, ts, reason),
	}
	if _, patchErr := repo.PatchAccounts(ctx, []AccountPatch{patch}); patchErr != nil {
		return false, patchErr
	}
	logInvalidCredentialsMarked(source, token, err)
	return true, nil
}

func FeedbackKindForError(err error) FeedbackKind {
	if err == nil {
		return FeedbackKindServerError
	}
	if protocol.IsInvalidCredentialsError(err) {
		return FeedbackKindUnauthorized
	}
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		return FeedbackKindServerError
	}
	switch {
	case upstream.Status == 429:
		return FeedbackKindRateLimited
	case upstream.Status == 401:
		return FeedbackKindUnauthorized
	case upstream.Status == 403:
		return FeedbackKindForbidden
	default:
		return FeedbackKindServerError
	}
}

func invalidCredentialsExt(records []AccountRecord, ts int64, reason string) map[string]any {
	ext := map[string]any{}
	if len(records) > 0 {
		for key, value := range records[0].Ext {
			ext[key] = value
		}
	}
	ext["expired_at"] = ts
	ext["expired_reason"] = reason
	return ext
}

func logInvalidCredentialsMarked(source string, token string, err error) {
	var upstream *platform.UpstreamError
	upstreamStatus := 0
	if errors.As(err, &upstream) {
		upstreamStatus = upstream.Status
	}
	logging.Logger.Info(
		"account expired from invalid credentials",
		"source", source,
		"token", tokenPrefix(token),
		"status", AccountStatusExpired,
		"upstream_status", upstreamStatus,
	)
}

func tokenPrefix(token string) string {
	if len(token) <= 10 {
		return token
	}
	return token[:10]
}
