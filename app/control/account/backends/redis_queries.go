package backends

import (
	"context"
	"sort"

	account "github.com/dslzl/gork/app/control/account"
)

func (r *RedisAccountRepository) RuntimeSnapshot(ctx context.Context) (account.RuntimeSnapshot, error) {
	keys, err := r.store.ScanKeys(ctx, "accounts:record:*")
	if err != nil {
		return account.RuntimeSnapshot{}, err
	}
	items := []account.AccountRecord{}
	for _, key := range keys {
		record, ok, err := r.getRecordByToken(ctx, redisTokenFromRecordKey(key))
		if err != nil {
			return account.RuntimeSnapshot{}, err
		}
		if ok && !record.IsDeleted() {
			items = append(items, record)
		}
	}
	revision, err := r.GetRevision(ctx)
	return account.RuntimeSnapshot{Revision: revision, Items: items}, err
}

func (r *RedisAccountRepository) ScanChanges(
	ctx context.Context,
	sinceRevision int,
	limit int,
) (account.AccountChangeSet, error) {
	if limit <= 0 {
		limit = account.AccountScanChangesDefaultLimit
	}
	revision, err := r.GetRevision(ctx)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	tokens, err := r.store.ZRangeByScore(ctx, redisKeyRevisionLog, sinceRevision, 0)
	if err != nil {
		return account.AccountChangeSet{}, err
	}
	return r.changeSetForTokens(ctx, tokens, sinceRevision, revision)
}

func (r *RedisAccountRepository) GetAccounts(
	ctx context.Context,
	tokens []string,
) ([]account.AccountRecord, error) {
	items := []account.AccountRecord{}
	for _, token := range tokens {
		record, ok, err := r.getRecordByToken(ctx, token)
		if err != nil {
			return nil, err
		}
		if ok {
			items = append(items, record)
		}
	}
	return items, nil
}

func (r *RedisAccountRepository) ListAccounts(
	ctx context.Context,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	query = normalizeLocalListQuery(query)
	ready, err := r.indexesReady(ctx)
	if err != nil {
		return account.AccountPage{}, err
	}
	if ready {
		return r.listAccountsIndexed(ctx, query)
	}
	return r.listAccountsScan(ctx, query)
}

func (r *RedisAccountRepository) changeSetForTokens(
	ctx context.Context,
	tokens []string,
	sinceRevision int,
	revision int,
) (account.AccountChangeSet, error) {
	changes := account.NewAccountChangeSet()
	changes.Revision = revision
	records := map[string]account.AccountRecord{}
	nextRevision := 0
	for _, token := range tokens {
		record, ok, err := r.getRecordByToken(ctx, token)
		if err != nil {
			return account.AccountChangeSet{}, err
		}
		if !ok {
			continue
		}
		if record.Revision <= sinceRevision {
			continue
		}
		records[token] = record
		if nextRevision == 0 || record.Revision < nextRevision {
			nextRevision = record.Revision
		}
	}
	if nextRevision == 0 {
		return changes, nil
	}
	changes.Revision = nextRevision
	for _, token := range tokens {
		record, ok := records[token]
		if !ok || record.Revision != nextRevision {
			continue
		}
		if record.IsDeleted() {
			changes.DeletedTokens = append(changes.DeletedTokens, record.Token)
		} else {
			changes.Items = append(changes.Items, record)
		}
	}
	changes.HasMore = nextRevision < revision
	return changes, nil
}

func (r *RedisAccountRepository) getRecordByToken(
	ctx context.Context,
	token string,
) (account.AccountRecord, bool, error) {
	hash, err := r.store.HGetAll(ctx, redisRecordKey(token))
	if err != nil || len(hash) == 0 {
		return account.AccountRecord{}, false, err
	}
	record, err := redisRecordFromHash(token, hash)
	return record, err == nil, err
}

func (r *RedisAccountRepository) listAccountsScan(
	ctx context.Context,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	keys, err := r.store.ScanKeys(ctx, "accounts:record:*")
	if err != nil {
		return account.AccountPage{}, err
	}
	records := []account.AccountRecord{}
	for _, key := range keys {
		record, ok, err := r.getRecordByToken(ctx, redisTokenFromRecordKey(key))
		if err != nil {
			return account.AccountPage{}, err
		}
		if ok && redisMatchesQuery(record, query) {
			records = append(records, record)
		}
	}
	return r.pageRedisRecords(ctx, records, query)
}

func (r *RedisAccountRepository) listAccountsIndexed(
	ctx context.Context,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	tokens, err := r.candidateTokens(ctx, query)
	if err != nil {
		return account.AccountPage{}, err
	}
	sort.Strings(tokens)
	records, err := r.GetAccounts(ctx, tokens)
	if err != nil {
		return account.AccountPage{}, err
	}
	filtered := []account.AccountRecord{}
	for _, record := range records {
		if redisMatchesQuery(record, query) {
			filtered = append(filtered, record)
		}
	}
	return r.pageRedisRecords(ctx, filtered, query)
}

func (r *RedisAccountRepository) pageRedisRecords(
	ctx context.Context,
	records []account.AccountRecord,
	query account.ListAccountsQuery,
) (account.AccountPage, error) {
	sortRedisRecords(records, query.SortBy, query.SortDesc)
	total := len(records)
	start := (query.Page - 1) * query.PageSize
	end := minInt(total, start+query.PageSize)
	if start > total {
		start, end = total, total
	}
	revision, err := r.GetRevision(ctx)
	if err != nil {
		return account.AccountPage{}, err
	}
	return account.AccountPage{
		Items:      records[start:end],
		Total:      total,
		Page:       query.Page,
		PageSize:   query.PageSize,
		TotalPages: maxInt(1, (total+query.PageSize-1)/query.PageSize),
		Revision:   revision,
	}, nil
}

func redisMatchesQuery(record account.AccountRecord, query account.ListAccountsQuery) bool {
	if !query.IncludeDeleted && record.IsDeleted() {
		return false
	}
	if query.Pool != nil && record.Pool != *query.Pool {
		return false
	}
	if query.Status != nil && record.Status != *query.Status {
		return false
	}
	for _, tag := range query.Tags {
		if !hasString(record.Tags, tag) {
			return false
		}
	}
	for _, tag := range query.ExcludeTags {
		if hasString(record.Tags, tag) {
			return false
		}
	}
	return true
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
