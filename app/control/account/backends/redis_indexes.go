package backends

import (
	"context"
	"sort"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

func (r *RedisAccountRepository) indexesReady(ctx context.Context) (bool, error) {
	value, ok, err := r.store.Get(ctx, redisKeyIndexReady)
	return ok && value != "", err
}

func (r *RedisAccountRepository) rebuildIndexes(ctx context.Context) error {
	keys, err := r.store.ScanKeys(ctx, "accounts:record:*")
	if err != nil {
		return err
	}
	if _, err := r.store.SetNX(ctx, redisKeyIndexReady, "1"); err != nil {
		return err
	}
	for _, key := range keys {
		token := redisTokenFromRecordKey(key)
		record, ok, err := r.getRecordByToken(ctx, token)
		if err != nil {
			return err
		}
		if ok {
			if err := r.addRecordIndexes(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RedisAccountRepository) candidateTokens(
	ctx context.Context,
	query account.ListAccountsQuery,
) ([]string, error) {
	baseKey := redisKeyIndexLive
	if query.IncludeDeleted {
		baseKey = redisKeyIndexAll
	}
	candidates, err := r.setMembers(ctx, baseKey)
	if err != nil {
		return nil, err
	}
	for _, key := range redisCandidateSetKeys(query) {
		members, err := r.setMembers(ctx, key)
		if err != nil {
			return nil, err
		}
		candidates = intersectStringSet(candidates, members)
	}
	for _, tag := range query.ExcludeTags {
		members, err := r.setMembers(ctx, redisIndexTagKey(tag))
		if err != nil {
			return nil, err
		}
		for member := range members {
			delete(candidates, member)
		}
	}
	return sortedSetMembers(candidates), nil
}

func redisCandidateSetKeys(query account.ListAccountsQuery) []string {
	keys := []string{}
	if query.Pool != nil && *query.Pool != "" {
		keys = append(keys, redisIndexPoolKey(*query.Pool))
	}
	if query.Status != nil {
		keys = append(keys, redisIndexStatusKey(query.Status.String()))
	}
	for _, tag := range query.Tags {
		keys = append(keys, redisIndexTagKey(tag))
	}
	return keys
}

func (r *RedisAccountRepository) setMembers(ctx context.Context, key string) (map[string]struct{}, error) {
	members, err := r.store.SMembers(ctx, key)
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, member := range members {
		out[member] = struct{}{}
	}
	return out, nil
}

func (r *RedisAccountRepository) removeRecordIndexes(
	ctx context.Context,
	record account.AccountRecord,
) error {
	token := record.Token
	if err := r.store.SRem(ctx, redisKeyIndexAll, token); err != nil {
		return err
	}
	if err := r.store.SRem(ctx, redisKeyIndexLive, token); err != nil {
		return err
	}
	if err := r.store.SRem(ctx, redisKeyIndexDeleted, token); err != nil {
		return err
	}
	return r.removeSecondaryIndexes(ctx, record)
}

func (r *RedisAccountRepository) removeSecondaryIndexes(
	ctx context.Context,
	record account.AccountRecord,
) error {
	token := record.Token
	for _, key := range redisRecordSetKeys(record) {
		if err := r.store.SRem(ctx, key, token); err != nil {
			return err
		}
	}
	for _, field := range redisSortFields {
		if err := r.store.ZRem(ctx, redisSortKey(field), token); err != nil {
			return err
		}
	}
	return nil
}

func (r *RedisAccountRepository) addRecordIndexes(ctx context.Context, record account.AccountRecord) error {
	token := record.Token
	if _, err := r.store.SetNX(ctx, redisKeyIndexReady, "1"); err != nil {
		return err
	}
	for _, key := range redisRecordSetKeys(record) {
		if err := r.store.SAdd(ctx, key, token); err != nil {
			return err
		}
	}
	if record.IsDeleted() {
		if err := r.store.SAdd(ctx, redisKeyIndexDeleted, token); err != nil {
			return err
		}
	} else {
		if err := r.store.SAdd(ctx, redisKeyIndexLive, token); err != nil {
			return err
		}
		if err := r.store.SAdd(ctx, redisPoolKey(record.Pool), token); err != nil {
			return err
		}
	}
	return r.addSortIndexes(ctx, record)
}

func redisRecordSetKeys(record account.AccountRecord) []string {
	keys := []string{
		redisKeyIndexAll,
		redisIndexPoolKey(record.Pool),
		redisIndexStatusKey(record.Status.String()),
	}
	for _, tag := range record.Tags {
		keys = append(keys, redisIndexTagKey(tag))
	}
	return keys
}

func (r *RedisAccountRepository) addSortIndexes(ctx context.Context, record account.AccountRecord) error {
	for _, field := range redisSortFields {
		if err := r.store.ZAdd(ctx, redisSortKey(field), map[string]int{record.Token: redisSortValue(record, field)}); err != nil {
			return err
		}
	}
	return nil
}

func intersectStringSet(a map[string]struct{}, b map[string]struct{}) map[string]struct{} {
	out := map[string]struct{}{}
	for item := range a {
		if _, ok := b[item]; ok {
			out[item] = struct{}{}
		}
	}
	return out
}

func sortedSetMembers(set map[string]struct{}) []string {
	out := []string{}
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func hasString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
