package account

import (
	"context"
	"errors"

	"github.com/jiujiu532/grok2api/app/platform"
)

func (s *AccountRefreshService) refreshOne(ctx context.Context, record AccountRecord, applyFallback bool) (RefreshResult, error) {
	if record.IsDeleted() {
		return RefreshResult{}, nil
	}
	windows, err := s.fetchAllQuotas(ctx, record.Token, record.Pool)
	if err != nil {
		marked, markErr := s.expireInvalidCredentials(ctx, record, err)
		if markErr != nil || marked {
			return RefreshResult{Checked: 1, Expired: 1}, markErr
		}
		return RefreshResult{}, err
	}
	if windows == nil {
		if !applyFallback {
			return RefreshResult{Checked: 1, Failed: 1}, nil
		}
		return s.applyFallback(ctx, record)
	}
	return s.applyFetchedWindows(ctx, record, windows, applyFallback)
}

func (s *AccountRefreshService) applyFetchedWindows(
	ctx context.Context,
	record AccountRecord,
	windows map[int]QuotaWindow,
	applyFallback bool,
) (RefreshResult, error) {
	quotaSet, err := record.QuotaSet()
	if err != nil {
		return RefreshResult{}, err
	}
	now := refreshNowMS()
	patch := AccountPatch{Token: record.Token}
	refreshed := false
	for _, modeID := range allRefreshModeIDs {
		if window, ok := windows[modeID]; ok {
			normalized := NormalizeQuotaWindow(record.Pool, modeID, &window)
			if normalized == nil {
				continue
			}
			setQuotaPatch(&patch, modeID, normalized.ToDict())
			refreshed = true
			continue
		}
		if applyFallback {
			applyFallbackWindowPatch(&patch, quotaSet, record.Pool, modeID, now)
		}
	}
	return s.patchFetchedResult(ctx, record, patch, windows, refreshed)
}

func (s *AccountRefreshService) patchFetchedResult(
	ctx context.Context,
	record AccountRecord,
	patch AccountPatch,
	windows map[int]QuotaWindow,
	refreshed bool,
) (RefreshResult, error) {
	if !hasQuotaPatch(patch) {
		failed := 1
		if refreshed {
			failed = 0
		}
		return RefreshResult{Checked: 1, Failed: failed}, nil
	}
	now := refreshNowMS()
	if inferred := InferPool(windows); inferred != record.Pool {
		patch.Pool = &inferred
	}
	if refreshed {
		one := 1
		patch.LastSyncAt = &now
		patch.UsageSyncDelta = &one
	}
	if _, err := s.repo.PatchAccounts(ctx, []AccountPatch{patch}); err != nil {
		return RefreshResult{}, err
	}
	recovered := 0
	if record.Status == AccountStatusCooling && refreshed {
		recovered = 1
	}
	failed := 1
	if refreshed {
		failed = 0
	}
	return RefreshResult{Checked: 1, Refreshed: boolInt(refreshed), Failed: failed, Recovered: recovered}, nil
}

func (s *AccountRefreshService) applyFallback(ctx context.Context, record AccountRecord) (RefreshResult, error) {
	quotaSet, err := record.QuotaSet()
	if err != nil {
		return RefreshResult{}, err
	}
	now := refreshNowMS()
	patch := AccountPatch{Token: record.Token}
	for _, modeID := range allRefreshModeIDs {
		applyFallbackWindowPatch(&patch, quotaSet, record.Pool, modeID, now)
	}
	if hasQuotaPatch(patch) {
		if _, err := s.repo.PatchAccounts(ctx, []AccountPatch{patch}); err != nil {
			return RefreshResult{}, err
		}
	}
	return RefreshResult{Checked: 1, Failed: 1}, nil
}

func (s *AccountRefreshService) RecordFailureAsync(ctx context.Context, token string, modeID int, err error) error {
	if err != nil {
		if done := s.recordSpecificFailure(ctx, token, modeID, err); done {
			return nil
		}
	}
	one := 1
	now := refreshNowMS()
	_, patchErr := s.repo.PatchAccounts(ctx, []AccountPatch{{
		Token:          token,
		UsageFailDelta: &one,
		LastFailAt:     &now,
	}})
	return swallowRefreshError(patchErr)
}

func (s *AccountRefreshService) recordSpecificFailure(ctx context.Context, token string, modeID int, err error) bool {
	records, getErr := s.repo.GetAccounts(ctx, []string{token})
	if getErr != nil || len(records) == 0 {
		return false
	}
	record := records[0]
	marked, markErr := s.expireInvalidCredentials(ctx, record, err)
	if markErr == nil && marked {
		return true
	}
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 429 || !isKnownRefreshMode(modeID) {
		return false
	}
	_ = s.patchRateLimitedFailure(ctx, record, modeID)
	return true
}

func (s *AccountRefreshService) patchRateLimitedFailure(ctx context.Context, record AccountRecord, modeID int) error {
	one := 1
	now := refreshNowMS()
	reason := "rate_limited"
	patch := AccountPatch{Token: record.Token, UsageFailDelta: &one, LastFailAt: &now, LastFailReason: &reason}
	if quotaSet, err := record.QuotaSet(); err == nil {
		if window := quotaSet.Get(modeID); window != nil {
			setQuotaPatch(&patch, modeID, rateLimitedWindowPatch(*window, now).ToDict())
		}
	}
	_, err := s.repo.PatchAccounts(ctx, []AccountPatch{patch})
	return swallowRefreshError(err)
}

func (s *AccountRefreshService) applySingleMode(
	ctx context.Context,
	record AccountRecord,
	modeID int,
	window *QuotaWindow,
	isUse bool,
	useAtMS int64,
) error {
	if !isKnownRefreshMode(modeID) {
		return nil
	}
	patch, err := s.singleModePatch(record, modeID, window, isUse, useAtMS)
	if err != nil || patch == nil {
		return err
	}
	_, err = s.repo.PatchAccounts(ctx, []AccountPatch{*patch})
	return err
}

func (s *AccountRefreshService) singleModePatch(
	record AccountRecord,
	modeID int,
	window *QuotaWindow,
	isUse bool,
	useAtMS int64,
) (*AccountPatch, error) {
	quotaSet, err := record.QuotaSet()
	if err != nil {
		return nil, err
	}
	patch := AccountPatch{Token: record.Token}
	if window != nil {
		if normalized := NormalizeQuotaWindow(record.Pool, modeID, window); normalized != nil {
			setQuotaPatch(&patch, modeID, normalized.ToDict())
			now := refreshNowMS()
			one := 1
			patch.LastSyncAt = &now
			patch.UsageSyncDelta = &one
		} else {
			return nil, nil
		}
	} else if existing := quotaSet.Get(modeID); existing != nil {
		setQuotaPatch(&patch, modeID, localUseWindowPatch(record.Pool, modeID, *existing).ToDict())
	}
	if isUse {
		one := 1
		patch.UsageUseDelta = &one
		patch.LastUseAt = &useAtMS
	}
	return &patch, nil
}

func (s *AccountRefreshService) expireInvalidCredentials(ctx context.Context, record AccountRecord, err error) (bool, error) {
	return MarkAccountInvalidCredentials(ctx, s.repo, record.Token, err, "usage refresh")
}
