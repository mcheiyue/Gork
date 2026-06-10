package backends

import (
	"context"
	"strconv"

	account "github.com/jiujiu532/grok2api/app/control/account"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func (r *RedisAccountRepository) UpsertAccounts(
	ctx context.Context,
	items []account.AccountUpsert,
) (account.AccountMutationResult, error) {
	if len(items) == 0 {
		return account.AccountMutationResult{}, nil
	}
	revision, err := r.bumpRevision(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := r.upsertRedisAccounts(ctx, items, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Upserted: count, Revision: revision}, nil
}

func (r *RedisAccountRepository) PatchAccounts(
	ctx context.Context,
	patches []account.AccountPatch,
) (account.AccountMutationResult, error) {
	if len(patches) == 0 {
		return account.AccountMutationResult{}, nil
	}
	revision, err := r.bumpRevision(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := r.patchRedisAccounts(ctx, patches, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Patched: count, Revision: revision}, nil
}

func (r *RedisAccountRepository) DeleteAccounts(
	ctx context.Context,
	tokens []string,
) (account.AccountMutationResult, error) {
	if len(tokens) == 0 {
		return account.AccountMutationResult{}, nil
	}
	revision, err := r.bumpRevision(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	count, err := r.deleteRedisAccounts(ctx, tokens, revision)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{Deleted: count, Revision: revision}, nil
}

func (r *RedisAccountRepository) ReplacePool(
	ctx context.Context,
	command account.BulkReplacePoolCommand,
) (account.AccountMutationResult, error) {
	command.Normalize()
	existing, err := r.store.SMembers(ctx, redisPoolKey(command.Pool))
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	deleted, err := r.DeleteAccounts(ctx, existing)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	upserted, err := r.UpsertAccounts(ctx, command.Upserts)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{
		Upserted: upserted.Upserted,
		Deleted:  deleted.Deleted,
		Revision: upserted.Revision,
	}, nil
}

func (r *RedisAccountRepository) upsertRedisAccounts(
	ctx context.Context,
	items []account.AccountUpsert,
	revision int,
) (int, error) {
	count := 0
	for _, item := range items {
		token, pool, ok := normalizeLocalUpsert(item)
		if !ok {
			continue
		}
		affected, err := r.upsertRedisAccount(ctx, item, token, pool, revision)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func (r *RedisAccountRepository) upsertRedisAccount(
	ctx context.Context,
	item account.AccountUpsert,
	token string,
	pool string,
	revision int,
) (int, error) {
	item.Normalize()
	ts := platformruntime.NowMS()
	record, err := account.NewAccountRecord(account.AccountRecord{
		Token:     token,
		Pool:      pool,
		Tags:      item.Tags,
		Ext:       item.Ext,
		Quota:     account.DefaultQuotaSet(pool).ToDict(),
		CreatedAt: ts,
		UpdatedAt: ts,
	})
	if err != nil {
		return 0, err
	}
	key := redisRecordKey(token)
	if old, err := r.store.HGetAll(ctx, key); err != nil {
		return 0, err
	} else if len(old) > 0 {
		oldRecord, err := redisRecordFromHash(token, old)
		if err != nil {
			return 0, err
		}
		if err := r.removeRecordIndexes(ctx, oldRecord); err != nil {
			return 0, err
		}
	}
	hash, err := redisHashFromRecord(record, revision)
	if err != nil {
		return 0, err
	}
	if err := r.store.HSet(ctx, key, hash); err != nil {
		return 0, err
	}
	if err := r.addRecordIndexes(ctx, record); err != nil {
		return 0, err
	}
	return 1, r.store.ZAdd(ctx, redisKeyRevisionLog, map[string]int{token: revision})
}

func (r *RedisAccountRepository) patchRedisAccounts(
	ctx context.Context,
	patches []account.AccountPatch,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	for _, patch := range patches {
		affected, err := r.patchRedisAccount(ctx, patch, ts, revision)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func (r *RedisAccountRepository) patchRedisAccount(
	ctx context.Context,
	patch account.AccountPatch,
	ts int64,
	revision int,
) (int, error) {
	key := redisRecordKey(patch.Token)
	hash, err := r.store.HGetAll(ctx, key)
	if err != nil || len(hash) == 0 {
		return 0, err
	}
	record, err := redisRecordFromHash(patch.Token, hash)
	if err != nil {
		return 0, err
	}
	if err := r.removeRecordIndexes(ctx, record); err != nil {
		return 0, err
	}
	updates, err := redisPatchUpdates(record, patch, ts, revision)
	if err != nil {
		return 0, err
	}
	if err := r.store.HSet(ctx, key, updates); err != nil {
		return 0, err
	}
	updatedHash, err := r.store.HGetAll(ctx, key)
	if err != nil || len(updatedHash) == 0 {
		return 0, err
	}
	updated, err := redisRecordFromHash(patch.Token, updatedHash)
	if err != nil {
		return 0, err
	}
	if err := r.addRecordIndexes(ctx, updated); err != nil {
		return 0, err
	}
	return 1, r.store.ZAdd(ctx, redisKeyRevisionLog, map[string]int{patch.Token: revision})
}

func redisPatchUpdates(
	record account.AccountRecord,
	patch account.AccountPatch,
	ts int64,
	revision int,
) (map[string]string, error) {
	updates := map[string]string{"updated_at": formatInt64(ts), "revision": strconv.Itoa(revision)}
	appendRedisBasicUpdates(updates, patch)
	appendRedisUsageUpdates(updates, record, patch)
	if err := appendRedisQuotaUpdates(updates, patch); err != nil {
		return nil, err
	}
	updates["tags"] = patchedRedisTags(record.Tags, patch)
	return appendRedisExtUpdates(updates, record, patch)
}
