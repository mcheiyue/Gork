package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type RedisTaskClient interface {
	HSet(ctx context.Context, key string, mapping map[string]string) error
	Expire(ctx context.Context, key string, ttlSeconds int) error
	HGetAll(ctx context.Context, key string) (map[string]any, error)
}

type RedisTaskSnapshotStoreOptions struct {
	KeyPrefix string
	TTLS      int
}

const defaultRedisTaskSnapshotTTLS = 300

type RedisTaskSnapshotStore struct {
	redis     RedisTaskClient
	keyPrefix string
	ttlS      int
	wg        sync.WaitGroup
}

func NewRedisTaskSnapshotStore(redis RedisTaskClient, options RedisTaskSnapshotStoreOptions) *RedisTaskSnapshotStore {
	prefix := options.KeyPrefix
	if prefix == "" {
		prefix = "runtime:task:"
	}
	ttlS := options.TTLS
	if ttlS == 0 {
		ttlS = defaultRedisTaskSnapshotTTLS
	}
	return &RedisTaskSnapshotStore{redis: redis, keyPrefix: prefix, ttlS: maxRuntimeInt(1, ttlS)}
}

func (s *RedisTaskSnapshotStore) Publish(task *AsyncTask, event map[string]any) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		_ = s.write(context.Background(), task, event)
	}()
}

func (s *RedisTaskSnapshotStore) Flush() {
	s.wg.Wait()
}

func (s *RedisTaskSnapshotStore) GetSnapshot(ctx context.Context, taskID string) (map[string]any, error) {
	raw, err := s.redis.HGetAll(ctx, s.keyPrefix+taskID)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	snapshot, ok := raw["snapshot"]
	if !ok || snapshot == nil {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(decodeTaskValue(snapshot)), &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func (s *RedisTaskSnapshotStore) write(ctx context.Context, task *AsyncTask, event map[string]any) error {
	mapping := map[string]string{
		"snapshot":   jsonCompact(task.Snapshot()),
		"updated_at": strconvTaskFloat(time.Now()),
	}
	if final := task.FinalEvent(); final != nil {
		mapping["final_event"] = jsonCompact(final)
	} else if event != nil {
		mapping["last_event"] = jsonCompact(event)
	}
	key := s.keyPrefix + task.ID
	if err := s.redis.HSet(ctx, key, mapping); err != nil {
		return err
	}
	return s.redis.Expire(ctx, key, s.ttlS)
}

func decodeTaskValue(value any) string {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func jsonCompact(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func strconvTaskFloat(t time.Time) string {
	return fmt.Sprintf("%g", float64(t.UnixNano())/1e9)
}
