package backends

import (
	"context"

	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func (r *RedisAccountRepository) deleteRedisAccounts(
	ctx context.Context,
	tokens []string,
	revision int,
) (int, error) {
	ts := platformruntime.NowMS()
	count := 0
	for _, token := range tokens {
		affected, err := r.deleteRedisAccount(ctx, token, ts, revision)
		if err != nil {
			return 0, err
		}
		count += affected
	}
	return count, nil
}

func (r *RedisAccountRepository) deleteRedisAccount(
	ctx context.Context,
	token string,
	ts int64,
	revision int,
) (int, error) {
	key := redisRecordKey(token)
	deletedAt, ok, err := r.store.HGet(ctx, key, "deleted_at")
	if err != nil || (ok && deletedAt != "" && deletedAt != "None") {
		return 0, err
	}
	hash, err := r.store.HGetAll(ctx, key)
	if err != nil || len(hash) == 0 {
		return 0, err
	}
	record, err := redisRecordFromHash(token, hash)
	if err != nil {
		return 0, err
	}
	if err := r.removeRecordIndexes(ctx, record); err != nil {
		return 0, err
	}
	updates := map[string]string{
		"deleted_at": formatInt64(ts),
		"updated_at": formatInt64(ts),
		"revision":   formatInt(revision),
	}
	if err := r.store.HSet(ctx, key, updates); err != nil {
		return 0, err
	}
	record.DeletedAt = &ts
	record.UpdatedAt = ts
	record.Revision = revision
	if err := r.addRecordIndexes(ctx, record); err != nil {
		return 0, err
	}
	return 1, r.store.ZAdd(ctx, redisKeyRevisionLog, map[string]int{token: revision})
}
