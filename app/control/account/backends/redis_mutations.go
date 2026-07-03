package backends

import (
	"context"
	"strconv"

	account "github.com/dslzl/gork/app/control/account"
	platformruntime "github.com/dslzl/gork/app/platform/runtime"
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
	backups, err := r.backupRedisRecords(ctx, existing)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	deletedRev, err := r.bumpRevision(ctx)
	if err != nil {
		return account.AccountMutationResult{}, err
	}
	deleted, err := r.deleteRedisAccounts(ctx, existing, deletedRev)
	if err != nil {
		_ = r.restoreRedisBackups(ctx, backups)
		return account.AccountMutationResult{}, err
	}
	upsertRev, err := r.bumpRevision(ctx)
	if err != nil {
		_ = r.restoreRedisBackups(ctx, backups)
		return account.AccountMutationResult{}, err
	}
	upserted, err := r.upsertRedisAccounts(ctx, command.Upserts, upsertRev)
	if err != nil {
		_ = r.restoreRedisBackups(ctx, backups)
		return account.AccountMutationResult{}, err
	}
	return account.AccountMutationResult{
		Upserted: upserted,
		Deleted:  deleted,
		Revision: upsertRev,
	}, nil
}

func (r *RedisAccountRepository) upsertRedisAccounts(
	ctx context.Context,
	items []account.AccountUpsert,
	revision int,
) (int, error) {
	count := 0
	backups := map[string]redisRecordBackup{}
	for _, item := range items {
		token, pool, ok := normalizeLocalUpsert(item)
		if !ok {
			continue
		}
		if _, ok := backups[token]; !ok {
			backup, err := r.backupRedisRecord(ctx, token)
			if err != nil {
				_ = r.restoreRedisBackups(ctx, backups)
				return 0, err
			}
			backups[token] = backup
		}
		affected, err := r.upsertRedisAccount(ctx, item, token, pool, revision)
		if err != nil {
			_ = r.restoreRedisBackups(ctx, backups)
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
	oldHash, err := r.store.HGetAll(ctx, key)
	if err != nil {
		return 0, err
	}
	var oldRecord *account.AccountRecord
	if len(oldHash) > 0 {
		parsed, err := redisRecordFromHash(token, oldHash)
		if err != nil {
			return 0, err
		}
		oldRecord = &parsed
		if err := r.removeRecordIndexes(ctx, parsed); err != nil {
			return 0, err
		}
	}
	hash, err := redisHashFromRecord(record, revision)
	if err != nil {
		return 0, err
	}
	if err := r.store.HSet(ctx, key, hash); err != nil {
		_ = r.rollbackRedisUpsert(ctx, key, oldRecord, oldHash, record)
		return 0, err
	}
	if err := r.addRecordIndexes(ctx, record); err != nil {
		_ = r.rollbackRedisUpsert(ctx, key, oldRecord, oldHash, record)
		return 0, err
	}
	if err := r.store.ZAdd(ctx, redisKeyRevisionLog, map[string]int{token: revision}); err != nil {
		_ = r.rollbackRedisUpsert(ctx, key, oldRecord, oldHash, record)
		return 0, err
	}
	return 1, nil
}

type redisRecordBackup struct {
	token  string
	key    string
	hash   map[string]string
	record *account.AccountRecord
}

func (r *RedisAccountRepository) backupRedisRecords(ctx context.Context, tokens []string) (map[string]redisRecordBackup, error) {
	backups := map[string]redisRecordBackup{}
	for _, token := range tokens {
		if _, ok := backups[token]; ok {
			continue
		}
		backup, err := r.backupRedisRecord(ctx, token)
		if err != nil {
			return nil, err
		}
		backups[token] = backup
	}
	return backups, nil
}

func (r *RedisAccountRepository) backupRedisRecord(ctx context.Context, token string) (redisRecordBackup, error) {
	key := redisRecordKey(token)
	hash, err := r.store.HGetAll(ctx, key)
	if err != nil {
		return redisRecordBackup{}, err
	}
	backup := redisRecordBackup{token: token, key: key, hash: cloneRedisHash(hash)}
	if len(hash) == 0 {
		return backup, nil
	}
	record, err := redisRecordFromHash(token, hash)
	if err != nil {
		return redisRecordBackup{}, err
	}
	backup.record = &record
	return backup, nil
}

func (r *RedisAccountRepository) restoreRedisBackups(ctx context.Context, backups map[string]redisRecordBackup) error {
	for _, backup := range backups {
		if err := r.restoreRedisBackup(ctx, backup); err != nil {
			return err
		}
	}
	return nil
}

func (r *RedisAccountRepository) restoreRedisBackup(ctx context.Context, backup redisRecordBackup) error {
	if current, ok, err := r.getRecordByToken(ctx, backup.token); err != nil {
		return err
	} else if ok {
		_ = r.removeRecordIndexes(ctx, current)
	}
	_ = r.store.ZRem(ctx, redisKeyRevisionLog, backup.token)
	if backup.record == nil {
		return r.store.Del(ctx, backup.key)
	}
	if err := r.store.HSet(ctx, backup.key, backup.hash); err != nil {
		return err
	}
	if err := r.addRecordIndexes(ctx, *backup.record); err != nil {
		return err
	}
	return r.store.ZAdd(ctx, redisKeyRevisionLog, map[string]int{backup.token: backup.record.Revision})
}

func cloneRedisHash(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (r *RedisAccountRepository) rollbackRedisUpsert(
	ctx context.Context,
	key string,
	oldRecord *account.AccountRecord,
	oldHash map[string]string,
	newRecord account.AccountRecord,
) error {
	_ = r.removeRecordIndexes(ctx, newRecord)
	_ = r.store.ZRem(ctx, redisKeyRevisionLog, newRecord.Token)
	if oldRecord == nil {
		return r.store.Del(ctx, key)
	}
	if err := r.store.HSet(ctx, key, oldHash); err != nil {
		return err
	}
	if err := r.addRecordIndexes(ctx, *oldRecord); err != nil {
		return err
	}
	return r.store.ZAdd(ctx, redisKeyRevisionLog, map[string]int{oldRecord.Token: oldRecord.Revision})
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
