package backends

import (
	"context"
	"strconv"

	account "github.com/jiujiu532/grok2api/app/control/account"
	redis "github.com/redis/go-redis/v9"
)

type goRedisAccountStore struct {
	client *redis.Client
}

func newRedisRepository(rawURL string) (account.AccountRepository, error) {
	store, err := NewGoRedisAccountStore(rawURL)
	if err != nil {
		return nil, err
	}
	return NewRedisAccountRepository(store), nil
}

func NewGoRedisAccountStore(rawURL string) (RedisAccountStore, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &goRedisAccountStore{client: redis.NewClient(options)}, nil
}

func (s *goRedisAccountStore) Incr(ctx context.Context, key string) (int, error) {
	value, err := s.client.Incr(ctx, key).Result()
	return int(value), err
}

func (s *goRedisAccountStore) SetNX(ctx context.Context, key string, value string) (bool, error) {
	return s.client.SetNX(ctx, key, value, 0).Result()
}

func (s *goRedisAccountStore) Get(ctx context.Context, key string) (string, bool, error) {
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *goRedisAccountStore) ScanKeys(ctx context.Context, pattern string) ([]string, error) {
	iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
	keys := []string{}
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	return keys, iter.Err()
}

func (s *goRedisAccountStore) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return s.client.HGetAll(ctx, key).Result()
}

func (s *goRedisAccountStore) HGet(ctx context.Context, key string, field string) (string, bool, error) {
	value, err := s.client.HGet(ctx, key, field).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *goRedisAccountStore) HSet(ctx context.Context, key string, mapping map[string]string) error {
	return s.client.HSet(ctx, key, redisHashArgs(mapping)...).Err()
}

func (s *goRedisAccountStore) ZAdd(ctx context.Context, key string, members map[string]int) error {
	values := make([]redis.Z, 0, len(members))
	for member, score := range members {
		values = append(values, redis.Z{Member: member, Score: float64(score)})
	}
	return s.client.ZAdd(ctx, key, values...).Err()
}

func (s *goRedisAccountStore) ZRangeByScore(
	ctx context.Context,
	key string,
	minExclusive int,
	limit int,
) ([]string, error) {
	options := &redis.ZRangeBy{Min: "(" + strconv.Itoa(minExclusive), Max: "+inf"}
	if limit > 0 {
		options.Count = int64(limit)
	}
	return s.client.ZRangeByScore(ctx, key, options).Result()
}

func (s *goRedisAccountStore) ZRem(ctx context.Context, key string, members ...string) error {
	values := make([]any, len(members))
	for i, member := range members {
		values[i] = member
	}
	return s.client.ZRem(ctx, key, values...).Err()
}

func (s *goRedisAccountStore) SAdd(ctx context.Context, key string, members ...string) error {
	values := make([]any, len(members))
	for i, member := range members {
		values[i] = member
	}
	return s.client.SAdd(ctx, key, values...).Err()
}

func (s *goRedisAccountStore) SRem(ctx context.Context, key string, members ...string) error {
	values := make([]any, len(members))
	for i, member := range members {
		values[i] = member
	}
	return s.client.SRem(ctx, key, values...).Err()
}

func (s *goRedisAccountStore) SMembers(ctx context.Context, key string) ([]string, error) {
	return s.client.SMembers(ctx, key).Result()
}

func (s *goRedisAccountStore) Close(context.Context) error {
	return s.client.Close()
}

func redisHashArgs(mapping map[string]string) []any {
	args := make([]any, 0, len(mapping)*2)
	for key, value := range mapping {
		args = append(args, key, value)
	}
	return args
}
