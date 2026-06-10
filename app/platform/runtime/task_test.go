package runtime

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestAsyncTaskRecordFinishAndFanout(t *testing.T) {
	store := &fakeTaskSnapshotStore{}
	task := NewAsyncTask(3, AsyncTaskOptions{ID: "task-1", SnapshotStore: store})
	events := task.Attach()
	defer task.Detach(events)

	task.Record(true, TaskRecordOptions{
		Item:   "item-1",
		Detail: map[string]any{"size": int64(2)},
		Count:  2,
	})

	progress := <-events
	if progress["type"] != "progress" || progress["task_id"] != "task-1" ||
		progress["processed"] != 2 || progress["ok"] != 2 || progress["fail"] != 0 ||
		progress["item"] != "item-1" {
		t.Fatalf("progress event = %#v", progress)
	}

	task.Finish(map[string]any{"done": true})
	done := <-events
	if done["type"] != "done" || done["warning"] != nil || !reflect.DeepEqual(done["result"], map[string]any{"done": true}) {
		t.Fatalf("done event = %#v", done)
	}
	if task.Status != "done" || !reflect.DeepEqual(task.FinalEvent(), done) {
		t.Fatalf("task status=%q final=%#v", task.Status, task.FinalEvent())
	}

	snapshot := task.Snapshot()
	wantSnapshot := map[string]any{
		"task_id":   "task-1",
		"status":    "done",
		"total":     3,
		"processed": 2,
		"ok":        2,
		"fail":      0,
		"warning":   nil,
	}
	if !reflect.DeepEqual(snapshot, wantSnapshot) {
		t.Fatalf("snapshot=%#v want=%#v", snapshot, wantSnapshot)
	}
	if len(store.events) != 2 {
		t.Fatalf("snapshot publishes = %d, want 2", len(store.events))
	}
}

func TestAsyncTaskDropsFullListenerWithoutBlockingSnapshotPublish(t *testing.T) {
	store := &fakeTaskSnapshotStore{}
	task := NewAsyncTask(250, AsyncTaskOptions{ID: "task-full", SnapshotStore: store})
	events := task.Attach()
	defer task.Detach(events)

	for i := 0; i < 201; i++ {
		task.Record(true, TaskRecordOptions{})
	}

	if got := len(events); got != 200 {
		t.Fatalf("listener buffered events = %d, want 200", got)
	}
	if got := len(store.events); got != 201 {
		t.Fatalf("snapshot publishes = %d, want 201", got)
	}
	if task.Processed != 201 || task.OK != 201 || task.Fail != 0 {
		t.Fatalf("task counters processed=%d ok=%d fail=%d", task.Processed, task.OK, task.Fail)
	}
}

func TestAsyncTaskFailureCancellationAndGlobalStore(t *testing.T) {
	store := &fakeTaskSnapshotStore{}
	SetTaskSnapshotStore(store)
	defer SetTaskSnapshotStore(nil)

	task := CreateTask(2)
	if task.ID == "" || GetTask(task.ID) != task {
		t.Fatalf("created task not stored: %#v", task)
	}
	if len(store.events) != 1 || store.events[0] != nil {
		t.Fatalf("create should publish snapshot without event, events=%#v", store.events)
	}

	task.Record(false, TaskRecordOptions{Error: "bad"})
	task.FailTask("boom")
	if task.Status != "error" || task.Error != "boom" || task.FinalEvent()["type"] != "error" {
		t.Fatalf("failed task = %#v final=%#v", task, task.FinalEvent())
	}

	task.Cancel()
	if !task.Cancelled {
		t.Fatalf("Cancel should mark task cancelled")
	}
	task.FinishCancelled()
	if task.Status != "cancelled" || task.FinalEvent()["type"] != "cancelled" {
		t.Fatalf("cancelled task = %#v final=%#v", task, task.FinalEvent())
	}

	if err := ExpireTask(context.Background(), task.ID, time.Millisecond); err != nil {
		t.Fatalf("ExpireTask returned error: %v", err)
	}
	if got := GetTask(task.ID); got != nil {
		t.Fatalf("expired task = %#v", got)
	}
}

func TestRedisTaskSnapshotStorePersistsLastEventForRunningTask(t *testing.T) {
	redis := &fakeTaskRedis{}
	store := NewRedisTaskSnapshotStore(redis, RedisTaskSnapshotStoreOptions{})
	task := NewAsyncTask(2, AsyncTaskOptions{ID: "running"})

	store.Publish(task, map[string]any{"type": "progress", "processed": 1})
	store.Flush()

	if redis.hsetKey != "runtime:task:running" || redis.expireKey != "runtime:task:running" || redis.expireTTL != 300 {
		t.Fatalf("redis keys hset=%q expire=%q ttl=%d", redis.hsetKey, redis.expireKey, redis.expireTTL)
	}
	if redis.mapping["snapshot"] == "" || redis.mapping["last_event"] == "" || redis.mapping["updated_at"] == "" {
		t.Fatalf("redis mapping = %#v", redis.mapping)
	}
	if _, ok := redis.mapping["final_event"]; ok {
		t.Fatalf("running task should persist last_event instead of final_event: %#v", redis.mapping)
	}

	var lastEvent map[string]any
	if err := json.Unmarshal([]byte(redis.mapping["last_event"]), &lastEvent); err != nil {
		t.Fatalf("last_event JSON decode failed: %v", err)
	}
	if lastEvent["type"] != "progress" || lastEvent["processed"] != float64(1) {
		t.Fatalf("last_event = %#v", lastEvent)
	}
}

func TestRedisTaskSnapshotStorePublishesAndReadsSnapshot(t *testing.T) {
	redis := &fakeTaskRedis{}
	store := NewRedisTaskSnapshotStore(redis, RedisTaskSnapshotStoreOptions{KeyPrefix: "task:", TTLS: -5})
	task := NewAsyncTask(1, AsyncTaskOptions{ID: "abc"})
	task.Finish(map[string]any{"ok": true}, "warn")

	store.Publish(task, map[string]any{"type": "ignored"})
	store.Flush()

	if redis.hsetKey != "task:abc" || redis.expireKey != "task:abc" || redis.expireTTL != 1 {
		t.Fatalf("redis keys hset=%q expire=%q ttl=%d", redis.hsetKey, redis.expireKey, redis.expireTTL)
	}
	if redis.mapping["snapshot"] == "" || redis.mapping["final_event"] == "" || redis.mapping["updated_at"] == "" {
		t.Fatalf("redis mapping = %#v", redis.mapping)
	}
	if _, ok := redis.mapping["last_event"]; ok {
		t.Fatalf("final task should persist final_event instead of last_event: %#v", redis.mapping)
	}

	redis.raw = map[string]any{"snapshot": []byte(redis.mapping["snapshot"])}
	snapshot, err := store.GetSnapshot(context.Background(), "abc")
	if err != nil {
		t.Fatalf("GetSnapshot returned error: %v", err)
	}
	if snapshot["task_id"] != "abc" || snapshot["status"] != "done" {
		encoded, _ := json.Marshal(snapshot)
		t.Fatalf("snapshot = %s", encoded)
	}

	redis.raw = map[string]any{}
	snapshot, err = store.GetSnapshot(context.Background(), "missing")
	if err != nil {
		t.Fatalf("GetSnapshot missing returned error: %v", err)
	}
	if snapshot != nil {
		t.Fatalf("missing snapshot = %#v", snapshot)
	}
}

type fakeTaskSnapshotStore struct {
	events []map[string]any
}

func (s *fakeTaskSnapshotStore) Publish(_ *AsyncTask, event map[string]any) {
	s.events = append(s.events, event)
}

type fakeTaskRedis struct {
	mapping   map[string]string
	raw       map[string]any
	hsetKey   string
	expireKey string
	expireTTL int
}

func (r *fakeTaskRedis) HSet(_ context.Context, key string, mapping map[string]string) error {
	r.hsetKey = key
	r.mapping = mapping
	return nil
}

func (r *fakeTaskRedis) Expire(_ context.Context, key string, ttlSeconds int) error {
	r.expireKey = key
	r.expireTTL = ttlSeconds
	return nil
}

func (r *fakeTaskRedis) HGetAll(_ context.Context, key string) (map[string]any, error) {
	return r.raw, nil
}
