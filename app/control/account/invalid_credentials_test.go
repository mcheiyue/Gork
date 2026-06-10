package account

import (
	"context"
	"errors"
	"testing"

	"github.com/jiujiu532/grok2api/app/platform"
)

type fakeInvalidCredentialRepo struct {
	records    []AccountRecord
	patches    []AccountPatch
	getErr     error
	patchErr   error
	getCalls   int
	patchCalls int
}

func (r *fakeInvalidCredentialRepo) GetAccounts(_ context.Context, tokens []string) ([]AccountRecord, error) {
	r.getCalls++
	if len(tokens) != 1 {
		return nil, errors.New("unexpected token query")
	}
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.records, nil
}

func (r *fakeInvalidCredentialRepo) PatchAccounts(_ context.Context, patches []AccountPatch) (AccountMutationResult, error) {
	r.patchCalls++
	if r.patchErr != nil {
		return AccountMutationResult{}, r.patchErr
	}
	r.patches = append(r.patches, patches...)
	return AccountMutationResult{Patched: len(patches)}, nil
}

func TestFeedbackKindForErrorMapsInvalidCredentialsBeforeStatus(t *testing.T) {
	err := platform.NewUpstreamError("forbidden", 403, "token expired")

	got := FeedbackKindForError(err)

	if got != FeedbackKindUnauthorized {
		t.Fatalf("FeedbackKindForError(invalid credentials 403) = %q, want %q", got, FeedbackKindUnauthorized)
	}
}

func TestFeedbackKindForErrorMapsUpstreamStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FeedbackKind
	}{
		{name: "nil", err: nil, want: FeedbackKindServerError},
		{name: "rate limited", err: platform.NewUpstreamError("rate", 429, ""), want: FeedbackKindRateLimited},
		{name: "unauthorized", err: platform.NewUpstreamError("auth", 401, ""), want: FeedbackKindUnauthorized},
		{name: "forbidden", err: platform.NewUpstreamError("forbidden", 403, ""), want: FeedbackKindForbidden},
		{name: "server error", err: platform.NewUpstreamError("upstream", 503, ""), want: FeedbackKindServerError},
		{name: "plain error", err: errors.New("boom"), want: FeedbackKindServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FeedbackKindForError(tt.err); got != tt.want {
				t.Fatalf("FeedbackKindForError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarkAccountInvalidCredentialsIgnoresOtherErrors(t *testing.T) {
	repo := &fakeInvalidCredentialRepo{}

	marked, err := MarkAccountInvalidCredentials(context.Background(), repo, "tok-1", platform.NewUpstreamError("rate", 429, ""), "refresh")

	if err != nil {
		t.Fatalf("MarkAccountInvalidCredentials returned error: %v", err)
	}
	if marked {
		t.Fatal("MarkAccountInvalidCredentials marked a non-invalid credential error")
	}
	if len(repo.patches) != 0 {
		t.Fatalf("patch count = %d, want 0", len(repo.patches))
	}
	if repo.getCalls != 0 || repo.patchCalls != 0 {
		t.Fatalf("repo calls = get:%d patch:%d, want none", repo.getCalls, repo.patchCalls)
	}
}

func TestMarkAccountInvalidCredentialsPropagatesRepositoryErrors(t *testing.T) {
	invalid := platform.NewUpstreamError("bad", 400, "bad-credentials")
	getRepo := &fakeInvalidCredentialRepo{getErr: errors.New("get failed")}

	marked, err := MarkAccountInvalidCredentials(context.Background(), getRepo, "tok-1", invalid, "refresh")

	if err == nil || err.Error() != "get failed" {
		t.Fatalf("get error = %v, want get failed", err)
	}
	if marked {
		t.Fatal("get error should not mark account")
	}
	if getRepo.getCalls != 1 || getRepo.patchCalls != 0 {
		t.Fatalf("repo calls after get error = get:%d patch:%d", getRepo.getCalls, getRepo.patchCalls)
	}

	patchRepo := &fakeInvalidCredentialRepo{patchErr: errors.New("patch failed")}
	marked, err = MarkAccountInvalidCredentials(context.Background(), patchRepo, "tok-1", invalid, "refresh")

	if err == nil || err.Error() != "patch failed" {
		t.Fatalf("patch error = %v, want patch failed", err)
	}
	if marked {
		t.Fatal("patch error should not mark account")
	}
	if patchRepo.getCalls != 1 || patchRepo.patchCalls != 1 {
		t.Fatalf("repo calls after patch error = get:%d patch:%d", patchRepo.getCalls, patchRepo.patchCalls)
	}
}

func TestMarkAccountInvalidCredentialsExpiresAccountAndMergesExt(t *testing.T) {
	oldNow := invalidCredentialsNowMS
	invalidCredentialsNowMS = func() int64 { return 123456789 }
	t.Cleanup(func() { invalidCredentialsNowMS = oldNow })
	repo := &fakeInvalidCredentialRepo{
		records: []AccountRecord{{
			Ext: map[string]any{"keep": "value"},
		}},
	}

	marked, err := MarkAccountInvalidCredentials(
		context.Background(),
		repo,
		"token-abcdef",
		platform.NewUpstreamError("bad", 400, "bad-credentials"),
		"usage",
	)

	if err != nil {
		t.Fatalf("MarkAccountInvalidCredentials returned error: %v", err)
	}
	if !marked {
		t.Fatal("MarkAccountInvalidCredentials did not mark invalid credentials")
	}
	if len(repo.patches) != 1 {
		t.Fatalf("patch count = %d, want 1", len(repo.patches))
	}
	patch := repo.patches[0]
	if patch.Token != "token-abcdef" {
		t.Fatalf("patch token = %q, want token-abcdef", patch.Token)
	}
	if patch.Status == nil || *patch.Status != AccountStatusExpired {
		t.Fatalf("patch status = %v, want %q", patch.Status, AccountStatusExpired)
	}
	if patch.LastFailAt == nil || *patch.LastFailAt != 123456789 {
		t.Fatalf("last_fail_at = %v, want 123456789", patch.LastFailAt)
	}
	if patch.LastFailReason == nil || *patch.LastFailReason != "invalid_credentials" {
		t.Fatalf("last_fail_reason = %v, want invalid_credentials", patch.LastFailReason)
	}
	if patch.StateReason == nil || *patch.StateReason != "invalid_credentials" {
		t.Fatalf("state_reason = %v, want invalid_credentials", patch.StateReason)
	}
	if got := patch.ExtMerge["keep"]; got != "value" {
		t.Fatalf("ext keep = %v, want value", got)
	}
	if got := patch.ExtMerge["expired_at"]; got != int64(123456789) {
		t.Fatalf("expired_at = %v, want 123456789", got)
	}
	if got := patch.ExtMerge["expired_reason"]; got != "invalid_credentials" {
		t.Fatalf("expired_reason = %v, want invalid_credentials", got)
	}
}

func TestMarkAccountInvalidCredentialsExpiresWithoutExistingRecord(t *testing.T) {
	oldNow := invalidCredentialsNowMS
	invalidCredentialsNowMS = func() int64 { return 987654321 }
	t.Cleanup(func() { invalidCredentialsNowMS = oldNow })
	repo := &fakeInvalidCredentialRepo{}

	marked, err := MarkAccountInvalidCredentials(
		context.Background(),
		repo,
		"token-missing",
		platform.NewUpstreamError("bad", 403, "blocked-user"),
		"usage",
	)

	if err != nil {
		t.Fatalf("MarkAccountInvalidCredentials returned error: %v", err)
	}
	if !marked {
		t.Fatal("MarkAccountInvalidCredentials did not mark invalid credentials")
	}
	if len(repo.patches) != 1 {
		t.Fatalf("patch count = %d, want 1", len(repo.patches))
	}
	ext := repo.patches[0].ExtMerge
	if len(ext) != 2 {
		t.Fatalf("ext = %#v, want only expired metadata", ext)
	}
	if got := ext["expired_at"]; got != int64(987654321) {
		t.Fatalf("expired_at = %v, want 987654321", got)
	}
	if got := ext["expired_reason"]; got != "invalid_credentials" {
		t.Fatalf("expired_reason = %v, want invalid_credentials", got)
	}
}
