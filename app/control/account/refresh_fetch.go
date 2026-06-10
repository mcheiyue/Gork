package account

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

var refreshAccountLookupBatchSize = 500

func (s *AccountRefreshService) fetchAllQuotas(ctx context.Context, token string, pool string) (map[int]QuotaWindow, error) {
	windows, err := protocol.FetchAllUsageQuotas(ctx, token, SupportedModeIDs(pool), protocol.UsageFetchOptions{
		Fetcher:  s.fetcher,
		SyncedAt: refreshNowMS(),
	})
	if err != nil {
		if isPlatformUpstreamError(err) {
			return nil, err
		}
		return nil, nil
	}
	return usageWindowsToQuotaWindows(windows), nil
}

func (s *AccountRefreshService) fetchModeQuota(ctx context.Context, token string, pool string, modeID int) (*QuotaWindow, error) {
	if !SupportsMode(pool, modeID) {
		return nil, nil
	}
	window, err := protocol.FetchModeUsageQuota(ctx, token, modeID, protocol.UsageFetchOptions{
		Fetcher:  s.fetcher,
		SyncedAt: refreshNowMS(),
	})
	if err != nil {
		if isPlatformUpstreamError(err) {
			return nil, err
		}
		return nil, nil
	}
	if window == nil {
		return nil, nil
	}
	converted := usageWindowToQuotaWindow(*window)
	return &converted, nil
}

func (s *AccountRefreshService) RefreshOnImport(ctx context.Context, tokens []string) (RefreshResult, error) {
	records, err := s.getAccountsBatched(ctx, tokens)
	if err != nil {
		return RefreshResult{}, err
	}
	active := filterRefreshManageable(records)
	if len(active) == 0 {
		return RefreshResult{Checked: len(records)}, nil
	}
	results, err := s.runRefreshBatch(ctx, active, true)
	if err != nil {
		return RefreshResult{}, err
	}
	agg := RefreshResult{Checked: len(records)}
	mergeRefreshResults(&agg, results)
	return agg, nil
}

func (s *AccountRefreshService) RefreshCallAsync(ctx context.Context, token string, modeID int) error {
	records, err := s.repo.GetAccounts(ctx, []string{token})
	if err != nil || len(records) == 0 || records[0].IsDeleted() {
		return err
	}
	record := records[0]
	if modeID == 5 {
		return s.applySingleMode(ctx, record, modeID, nil, true, refreshNowMS())
	}
	window, err := s.fetchModeQuota(ctx, token, record.Pool, modeID)
	if err != nil {
		marked, markErr := s.expireInvalidCredentials(ctx, record, err)
		if markErr != nil || marked {
			return markErr
		}
		return err
	}
	return s.applySingleMode(ctx, record, modeID, window, true, refreshNowMS())
}

func (s *AccountRefreshService) RefreshScheduled(ctx context.Context, pool *string) (RefreshResult, error) {
	snapshot, err := s.repo.RuntimeSnapshot(ctx)
	if err != nil {
		return RefreshResult{}, err
	}
	records := filterRefreshManageable(snapshot.Items)
	if pool != nil {
		records = filterRefreshPool(records, *pool)
	}
	results, err := s.runRefreshBatch(ctx, records, true)
	if err != nil {
		return RefreshResult{}, err
	}
	agg := RefreshResult{}
	mergeRefreshResults(&agg, results)
	return agg, nil
}

func (s *AccountRefreshService) RefreshOnDemand(ctx context.Context) (RefreshResult, error) {
	if !s.tryStartOnDemand() {
		return RefreshResult{}, nil
	}
	success := false
	defer func() {
		s.finishOnDemand(success)
	}()
	result, err := s.RefreshScheduled(ctx, nil)
	if err == nil {
		success = true
	}
	return result, err
}

func (s *AccountRefreshService) RefreshTokens(ctx context.Context, tokens []string) (RefreshResult, error) {
	records, err := s.getAccountsBatched(ctx, tokens)
	if err != nil {
		return RefreshResult{}, err
	}
	results, err := s.runRefreshBatch(ctx, filterRefreshManageable(records), false)
	if err != nil {
		return RefreshResult{}, err
	}
	agg := RefreshResult{}
	mergeRefreshResults(&agg, results)
	return agg, nil
}

func (s *AccountRefreshService) getAccountsBatched(ctx context.Context, tokens []string) ([]AccountRecord, error) {
	if len(tokens) == 0 {
		return []AccountRecord{}, nil
	}
	limit := refreshAccountLookupBatchSize
	if limit <= 0 {
		limit = 500
	}
	records := []AccountRecord{}
	for start := 0; start < len(tokens); start += limit {
		end := start + limit
		if end > len(tokens) {
			end = len(tokens)
		}
		chunkRecords, err := s.repo.GetAccounts(ctx, tokens[start:end])
		if err != nil {
			return nil, err
		}
		records = append(records, chunkRecords...)
	}
	return records, nil
}

func (s *AccountRefreshService) runRefreshBatch(ctx context.Context, records []AccountRecord, applyFallback bool) ([]RefreshResult, error) {
	if len(records) == 0 {
		return []RefreshResult{}, nil
	}
	concurrency := maxInt(s.usageConcurrency, 1)
	if concurrency > len(records) {
		concurrency = len(records)
	}
	results := make([]RefreshResult, len(records))
	errs := make(chan error, len(records))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for idx, record := range records {
		idx, record := idx, record
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			result, err := s.refreshOne(ctx, record, applyFallback)
			if err != nil {
				errs <- err
				return
			}
			results[idx] = result
		}()
	}
	wg.Wait()
	close(errs)
	if err, ok := <-errs; ok {
		return nil, err
	}
	return results, nil
}

func (s *AccountRefreshService) tryStartOnDemand() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onDemandRunning || time.Since(s.onDemandLast) < s.onDemandMinInterval {
		return false
	}
	s.onDemandRunning = true
	return true
}

func (s *AccountRefreshService) finishOnDemand(success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDemandRunning = false
	if success {
		s.onDemandLast = time.Now()
	}
}

func filterRefreshManageable(records []AccountRecord) []AccountRecord {
	now := refreshNowMS()
	out := []AccountRecord{}
	for _, record := range records {
		if isRefreshManageable(record, now) {
			out = append(out, record)
		}
	}
	return out
}

func filterRefreshPool(records []AccountRecord, pool string) []AccountRecord {
	out := []AccountRecord{}
	for _, record := range records {
		if record.Pool == pool {
			out = append(out, record)
		}
	}
	return out
}

func mergeRefreshResults(agg *RefreshResult, results []RefreshResult) {
	for _, result := range results {
		agg.Merge(result)
	}
}

func usageWindowsToQuotaWindows(windows map[int]protocol.UsageQuotaWindow) map[int]QuotaWindow {
	out := map[int]QuotaWindow{}
	for modeID, window := range windows {
		out[modeID] = usageWindowToQuotaWindow(window)
	}
	return out
}

func usageWindowToQuotaWindow(window protocol.UsageQuotaWindow) QuotaWindow {
	return QuotaWindow{
		Remaining:     window.Remaining,
		Total:         window.Total,
		WindowSeconds: window.WindowSeconds,
		ResetAt:       &window.ResetAt,
		SyncedAt:      &window.SyncedAt,
		Source:        QuotaSourceReal,
	}
}

func isPlatformUpstreamError(err error) bool {
	var upstream *platform.UpstreamError
	return errors.As(err, &upstream)
}
