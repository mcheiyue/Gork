package runtime

import (
	"context"
	"testing"
)

func TestRedisRuntimeStoreAcquiresAndReleasesNamedLock(t *testing.T) {
	ctx := context.Background()
	redis := &fakeRuntimeRedis{acquired: true, locks: map[string]string{}}
	store := NewRedisRuntimeStore(redis, RedisRuntimeStoreOptions{KeyPrefix: "runtime:"})

	first, err := store.AcquireLock(ctx, "scheduler", RedisRuntimeLockOptions{Owner: "worker-a", TTLMS: 30_000})
	if err != nil {
		t.Fatalf("AcquireLock first returned error: %v", err)
	}
	second, err := store.AcquireLock(ctx, "scheduler", RedisRuntimeLockOptions{Owner: "worker-b", TTLMS: 30_000})
	if err != nil {
		t.Fatalf("AcquireLock second returned error: %v", err)
	}
	if first == nil || second != nil {
		t.Fatalf("lock acquisition first=%#v second=%#v", first, second)
	}
	if first.Key != "runtime:lock:scheduler" || first.Owner != "worker-a" || first.TTLMS != 30_000 {
		t.Fatalf("lease = %#v", first)
	}
	released, err := first.Release(ctx)
	if err != nil || !released {
		t.Fatalf("Release = %v/%v", released, err)
	}
	third, err := store.AcquireLock(ctx, "scheduler", RedisRuntimeLockOptions{Owner: "worker-b", TTLMS: 30_000})
	if err != nil {
		t.Fatalf("AcquireLock third returned error: %v", err)
	}
	if third == nil || third.Owner != "worker-b" {
		t.Fatalf("third lease = %#v", third)
	}
}
