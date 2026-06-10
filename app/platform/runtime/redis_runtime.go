package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

type RedisRuntimeClient interface {
	Get(ctx context.Context, key string) (any, error)
	SetNX(ctx context.Context, key, value string, ttlMS int) (bool, error)
	Expire(ctx context.Context, key string, ttlSeconds int) error
	Delete(ctx context.Context, key string) error
}

type RedisRuntimeClientFactory func(rawURL string) (RedisRuntimeClient, error)

const defaultRedisRuntimeLockTTLMS = 300000

type RedisRuntimeStoreOptions struct {
	KeyPrefix string
}

type RedisRuntimeLockOptions struct {
	Owner string
	TTLMS int
}

type RedisRuntimeLease struct {
	client   RedisRuntimeClient
	Key      string
	Owner    string
	TTLMS    int
	released bool
}

type RedisRuntimeStore struct {
	Redis     RedisRuntimeClient
	KeyPrefix string
}

type goRedisRuntimeClient struct {
	client *redis.Client
}

func newGoRedisRuntimeClient(rawURL string) (RedisRuntimeClient, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &goRedisRuntimeClient{client: redis.NewClient(options)}, nil
}

func NewRedisRuntimeLease(client RedisRuntimeClient, key, owner string, ttlMS int) *RedisRuntimeLease {
	return &RedisRuntimeLease{
		client: client,
		Key:    key,
		Owner:  owner,
		TTLMS:  maxRuntimeInt(1, ttlMS),
	}
}

func (l *RedisRuntimeLease) Renew(ctx context.Context) (bool, error) {
	if l.released {
		return false, nil
	}
	current, err := l.client.Get(ctx, l.Key)
	if err != nil {
		return false, err
	}
	owner, ok := decodeRedisRuntimeValue(current)
	if !ok || owner != l.Owner {
		return false, nil
	}
	if err := l.client.Expire(ctx, l.Key, maxRuntimeInt(1, l.TTLMS/1000)); err != nil {
		return false, err
	}
	return true, nil
}

func (l *RedisRuntimeLease) Release(ctx context.Context) (bool, error) {
	if l.released {
		return false, nil
	}
	current, err := l.client.Get(ctx, l.Key)
	if err != nil {
		return false, err
	}
	owner, ok := decodeRedisRuntimeValue(current)
	if !ok || owner != l.Owner {
		l.released = true
		return false, nil
	}
	if err := l.client.Delete(ctx, l.Key); err != nil {
		return false, err
	}
	l.released = true
	return true, nil
}

func NewRedisRuntimeStore(client RedisRuntimeClient, options RedisRuntimeStoreOptions) *RedisRuntimeStore {
	prefix := options.KeyPrefix
	if prefix == "" {
		prefix = "runtime:"
	}
	return &RedisRuntimeStore{Redis: client, KeyPrefix: prefix}
}

func (s *RedisRuntimeStore) Key(parts ...string) string {
	safe := []string{}
	for _, part := range parts {
		if part == "" {
			continue
		}
		safe = append(safe, strings.Trim(part, ":"))
	}
	return s.KeyPrefix + strings.Join(safe, ":")
}

func (s *RedisRuntimeStore) AcquireLock(ctx context.Context, name string, options RedisRuntimeLockOptions) (*RedisRuntimeLease, error) {
	owner := options.Owner
	if owner == "" {
		owner = runtimeOwner()
	}
	ttlMS := options.TTLMS
	if ttlMS == 0 {
		ttlMS = defaultRedisRuntimeLockTTLMS
	}
	ttlMS = maxRuntimeInt(1, ttlMS)
	key := s.Key("lock", name)
	acquired, err := s.Redis.SetNX(ctx, key, owner, ttlMS)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, nil
	}
	return NewRedisRuntimeLease(s.Redis, key, owner, ttlMS), nil
}

func (s *RedisRuntimeStore) Close(ctx context.Context) error {
	closer, ok := s.Redis.(interface {
		AClose(context.Context) error
	})
	if !ok {
		return nil
	}
	return closer.AClose(ctx)
}

func RuntimeRedisURL() string {
	if value := strings.TrimSpace(os.Getenv("RUNTIME_REDIS_URL")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("ACCOUNT_REDIS_URL"))
}

func CreateRuntimeStoreFromEnv(factory RedisRuntimeClientFactory) (*RedisRuntimeStore, error) {
	rawURL := RuntimeRedisURL()
	if rawURL == "" {
		return nil, nil
	}
	if factory == nil {
		factory = newGoRedisRuntimeClient
	}
	client, err := factory(rawURL)
	if err != nil {
		return nil, err
	}
	return NewRedisRuntimeStore(client, RedisRuntimeStoreOptions{}), nil
}

func (c *goRedisRuntimeClient) Get(ctx context.Context, key string) (any, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (c *goRedisRuntimeClient) SetNX(ctx context.Context, key, value string, ttlMS int) (bool, error) {
	ttl := time.Duration(maxRuntimeInt(1, ttlMS)) * time.Millisecond
	return c.client.SetNX(ctx, key, value, ttl).Result()
}

func (c *goRedisRuntimeClient) Expire(ctx context.Context, key string, ttlSeconds int) error {
	ttl := time.Duration(maxRuntimeInt(1, ttlSeconds)) * time.Second
	return c.client.Expire(ctx, key, ttl).Err()
}

func (c *goRedisRuntimeClient) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *goRedisRuntimeClient) HSet(ctx context.Context, key string, mapping map[string]string) error {
	return c.client.HSet(ctx, key, runtimeRedisHashArgs(mapping)...).Err()
}

func (c *goRedisRuntimeClient) HGetAll(ctx context.Context, key string) (map[string]any, error) {
	values, err := c.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(values))
	for field, value := range values {
		result[field] = value
	}
	return result, nil
}

func (c *goRedisRuntimeClient) AClose(context.Context) error {
	return c.client.Close()
}

func runtimeRedisHashArgs(mapping map[string]string) []any {
	args := make([]any, 0, len(mapping)*2)
	for key, value := range mapping {
		args = append(args, key, value)
	}
	return args
}

func decodeRedisRuntimeValue(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", false
	case []byte:
		return string(typed), true
	case string:
		return typed, true
	default:
		return fmt.Sprint(typed), true
	}
}

func runtimeOwner() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), NextHex(32))
}

func maxRuntimeInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
