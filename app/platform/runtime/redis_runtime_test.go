package runtime

import (
	"context"
	"testing"
)

func TestRedisRuntimeStoreKeyAndAcquireLockMatchPythonSemantics(t *testing.T) {
	client := &fakeRuntimeRedis{acquired: true}
	store := NewRedisRuntimeStore(client, RedisRuntimeStoreOptions{})

	if got, want := store.Key("lock", ":leader:", ""), "runtime:lock:leader"; got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}

	lease, err := store.AcquireLock(context.Background(), "leader", RedisRuntimeLockOptions{})
	if err != nil {
		t.Fatalf("AcquireLock returned error: %v", err)
	}
	if lease == nil {
		t.Fatalf("AcquireLock returned nil lease")
	}
	if client.setKey != "runtime:lock:leader" || client.setValue == "" || client.setPX != 300000 {
		t.Fatalf("set call key=%q value=%q px=%d", client.setKey, client.setValue, client.setPX)
	}
	if lease.Key != "runtime:lock:leader" || lease.Owner != client.setValue || lease.TTLMS != 300000 {
		t.Fatalf("lease = %#v", lease)
	}

	lease, err = store.AcquireLock(context.Background(), "leader", RedisRuntimeLockOptions{
		Owner: "owner-1",
		TTLMS: -5,
	})
	if err != nil {
		t.Fatalf("AcquireLock with explicit owner returned error: %v", err)
	}
	if client.setValue != "owner-1" || client.setPX != 1 || lease == nil || lease.Owner != "owner-1" || lease.TTLMS != 1 {
		t.Fatalf("explicit owner setValue=%q px=%d lease=%#v", client.setValue, client.setPX, lease)
	}

	client.acquired = false
	lease, err = store.AcquireLock(context.Background(), "leader", RedisRuntimeLockOptions{Owner: "owner-1", TTLMS: 300000})
	if err != nil {
		t.Fatalf("AcquireLock false returned error: %v", err)
	}
	if lease != nil {
		t.Fatalf("AcquireLock should return nil when Redis SET NX fails")
	}
}

func TestRedisRuntimeLeaseRenewAndReleaseCheckOwner(t *testing.T) {
	client := &fakeRuntimeRedis{value: []byte("owner-1")}
	lease := NewRedisRuntimeLease(client, "runtime:lock:leader", "owner-1", 1500)

	renewed, err := lease.Renew(context.Background())
	if err != nil {
		t.Fatalf("Renew returned error: %v", err)
	}
	if !renewed || client.expireKey != "runtime:lock:leader" || client.expireSeconds != 1 {
		t.Fatalf("renewed=%t expire=%q seconds=%d", renewed, client.expireKey, client.expireSeconds)
	}

	released, err := lease.Release(context.Background())
	if err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if !released || client.deletedKey != "runtime:lock:leader" {
		t.Fatalf("released=%t deleted=%q", released, client.deletedKey)
	}

	renewed, err = lease.Renew(context.Background())
	if err != nil {
		t.Fatalf("Renew after release returned error: %v", err)
	}
	if renewed {
		t.Fatalf("released lease should not renew")
	}
}

func TestRedisRuntimeLeaseRenewMismatchDoesNotMarkReleased(t *testing.T) {
	client := &fakeRuntimeRedis{value: "other-owner"}
	lease := NewRedisRuntimeLease(client, "runtime:lock:leader", "owner-1", 300000)

	renewed, err := lease.Renew(context.Background())
	if err != nil {
		t.Fatalf("Renew mismatch returned error: %v", err)
	}
	if renewed || client.expireKey != "" {
		t.Fatalf("mismatch renew=%t expire=%q", renewed, client.expireKey)
	}

	client.value = "owner-1"
	released, err := lease.Release(context.Background())
	if err != nil {
		t.Fatalf("Release after renew mismatch returned error: %v", err)
	}
	if !released || client.deletedKey != "runtime:lock:leader" {
		t.Fatalf("renew mismatch should not mark released: released=%t deleted=%q", released, client.deletedKey)
	}
}

func TestRedisRuntimeLeaseReleaseMismatchMarksReleased(t *testing.T) {
	client := &fakeRuntimeRedis{value: "other-owner"}
	lease := NewRedisRuntimeLease(client, "runtime:lock:leader", "owner-1", 300000)

	released, err := lease.Release(context.Background())
	if err != nil {
		t.Fatalf("Release mismatch returned error: %v", err)
	}
	if released || client.deletedKey != "" {
		t.Fatalf("mismatch release=%t deleted=%q", released, client.deletedKey)
	}

	client.value = "owner-1"
	released, err = lease.Release(context.Background())
	if err != nil {
		t.Fatalf("second Release returned error: %v", err)
	}
	if released {
		t.Fatalf("mismatch release should mark lease released")
	}
}

func TestRuntimeRedisURLAndCreateStoreFromEnv(t *testing.T) {
	t.Setenv("RUNTIME_REDIS_URL", " redis://runtime ")
	t.Setenv("ACCOUNT_REDIS_URL", "redis://account")
	if got := RuntimeRedisURL(); got != "redis://runtime" {
		t.Fatalf("RuntimeRedisURL() = %q", got)
	}

	t.Setenv("RUNTIME_REDIS_URL", "")
	if got := RuntimeRedisURL(); got != "redis://account" {
		t.Fatalf("RuntimeRedisURL fallback = %q", got)
	}

	var calledURL string
	store, err := CreateRuntimeStoreFromEnv(func(rawURL string) (RedisRuntimeClient, error) {
		calledURL = rawURL
		return &fakeRuntimeRedis{}, nil
	})
	if err != nil {
		t.Fatalf("CreateRuntimeStoreFromEnv returned error: %v", err)
	}
	if store == nil || calledURL != "redis://account" {
		t.Fatalf("store=%#v calledURL=%q", store, calledURL)
	}

	t.Setenv("RUNTIME_REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("ACCOUNT_REDIS_URL", "")
	store, err = CreateRuntimeStoreFromEnv(nil)
	if err != nil {
		t.Fatalf("CreateRuntimeStoreFromEnv default factory returned error: %v", err)
	}
	if store == nil {
		t.Fatalf("default factory store is nil")
	}
	if _, ok := store.Redis.(RedisTaskClient); !ok {
		t.Fatalf("default runtime Redis client should support task snapshots")
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("default factory store close returned error: %v", err)
	}

	t.Setenv("ACCOUNT_REDIS_URL", "")
	t.Setenv("RUNTIME_REDIS_URL", "")
	store, err = CreateRuntimeStoreFromEnv(func(rawURL string) (RedisRuntimeClient, error) {
		t.Fatalf("factory should not be called without URL")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("CreateRuntimeStoreFromEnv empty returned error: %v", err)
	}
	if store != nil {
		t.Fatalf("empty env store = %#v", store)
	}
}

func TestRedisRuntimeStoreCloseCallsOptionalAClose(t *testing.T) {
	client := &fakeRuntimeRedis{}
	store := NewRedisRuntimeStore(client, RedisRuntimeStoreOptions{})

	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !client.closed {
		t.Fatalf("Close should call AClose when available")
	}
}

func TestRedisRuntimeStoreCloseIgnoresMissingAClose(t *testing.T) {
	store := NewRedisRuntimeStore(noCloseRuntimeRedis{}, RedisRuntimeStoreOptions{})
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close without AClose returned error: %v", err)
	}
}

type fakeRuntimeRedis struct {
	value         any
	acquired      bool
	closed        bool
	setKey        string
	setValue      string
	setPX         int
	expireKey     string
	expireSeconds int
	deletedKey    string
	locks         map[string]string
}

func (r *fakeRuntimeRedis) Get(_ context.Context, key string) (any, error) {
	if r.locks != nil {
		value, ok := r.locks[key]
		if !ok {
			return nil, nil
		}
		return value, nil
	}
	return r.value, nil
}

func (r *fakeRuntimeRedis) SetNX(_ context.Context, key, value string, ttlMS int) (bool, error) {
	r.setKey = key
	r.setValue = value
	r.setPX = ttlMS
	if r.locks != nil {
		if _, exists := r.locks[key]; exists {
			return false, nil
		}
		r.locks[key] = value
		return true, nil
	}
	return r.acquired, nil
}

func (r *fakeRuntimeRedis) Expire(_ context.Context, key string, ttlSeconds int) error {
	r.expireKey = key
	r.expireSeconds = ttlSeconds
	return nil
}

func (r *fakeRuntimeRedis) Delete(_ context.Context, key string) error {
	r.deletedKey = key
	if r.locks != nil {
		delete(r.locks, key)
	}
	return nil
}

func (r *fakeRuntimeRedis) AClose(context.Context) error {
	r.closed = true
	return nil
}

type noCloseRuntimeRedis struct{}

func (noCloseRuntimeRedis) Get(context.Context, string) (any, error) {
	return nil, nil
}

func (noCloseRuntimeRedis) SetNX(context.Context, string, string, int) (bool, error) {
	return false, nil
}

func (noCloseRuntimeRedis) Expire(context.Context, string, int) error {
	return nil
}

func (noCloseRuntimeRedis) Delete(context.Context, string) error {
	return nil
}
