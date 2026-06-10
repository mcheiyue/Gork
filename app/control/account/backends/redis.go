package backends

import (
	"context"
	"strconv"
	"strings"
)

type RedisAccountRepository struct {
	store RedisAccountStore
}

func NewRedisAccountRepository(store RedisAccountStore) *RedisAccountRepository {
	return &RedisAccountRepository{store: store}
}

func (r *RedisAccountRepository) Initialize(ctx context.Context) error {
	if _, err := r.store.SetNX(ctx, redisKeyRevision, "0"); err != nil {
		return err
	}
	ready, err := r.indexesReady(ctx)
	if err != nil || ready {
		return err
	}
	return r.rebuildIndexes(ctx)
}

func (r *RedisAccountRepository) GetRevision(ctx context.Context) (int, error) {
	raw, ok, err := r.store.Get(ctx, redisKeyRevision)
	if err != nil || !ok || raw == "" {
		return 0, err
	}
	return strconv.Atoi(raw)
}

func (r *RedisAccountRepository) Close(ctx context.Context) error {
	return r.store.Close(ctx)
}

func (r *RedisAccountRepository) bumpRevision(ctx context.Context) (int, error) {
	return r.store.Incr(ctx, redisKeyRevision)
}

func redisTokenFromRecordKey(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) == 3 {
		return parts[2]
	}
	return key
}
