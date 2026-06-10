package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunBatchPreservesOrderAndBoundsConcurrency(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6}
	var active atomic.Int64
	var maxActive atomic.Int64

	results, err := RunBatch(context.Background(), items, func(_ context.Context, item int) (int, error) {
		now := active.Add(1)
		for {
			prev := maxActive.Load()
			if now <= prev || maxActive.CompareAndSwap(prev, now) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		return item * 10, nil
	}, 2, 0, 0)
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}
	if !reflect.DeepEqual(results, []int{10, 20, 30, 40, 50, 60}) {
		t.Fatalf("RunBatch results = %#v, want input order", results)
	}
	if got := maxActive.Load(); got > 2 {
		t.Fatalf("max concurrency = %d, want <= 2", got)
	}
}

func TestRunBatchHandlesEmptyInputAndMinimumConcurrency(t *testing.T) {
	results, err := RunBatch(context.Background(), []int{}, func(_ context.Context, item int) (int, error) {
		return item, nil
	}, 0, 0, 0)
	if err != nil {
		t.Fatalf("RunBatch returned error for empty input: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("RunBatch empty results length = %d, want 0", len(results))
	}

	results, err = RunBatch(context.Background(), []int{3}, func(_ context.Context, item int) (int, error) {
		return item + 1, nil
	}, 0, 0, 0)
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}
	if !reflect.DeepEqual(results, []int{4}) {
		t.Fatalf("RunBatch results = %#v, want []int{4}", results)
	}
}

func TestRunBatchProcessesChunksAndPausesBetweenBatches(t *testing.T) {
	started := make(chan int, 4)
	start := time.Now()

	results, err := RunBatch(context.Background(), []int{1, 2, 3, 4}, func(_ context.Context, item int) (int, error) {
		started <- int(time.Since(start).Milliseconds())
		return item, nil
	}, 10, 20*time.Millisecond, 2)
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}
	if !reflect.DeepEqual(results, []int{1, 2, 3, 4}) {
		t.Fatalf("RunBatch results = %#v, want input order", results)
	}

	firstBatchA := <-started
	firstBatchB := <-started
	secondBatchA := <-started
	secondBatchB := <-started
	if firstBatchA >= 15 || firstBatchB >= 15 {
		t.Fatalf("first batch started too late: %dms, %dms", firstBatchA, firstBatchB)
	}
	if secondBatchA < 15 || secondBatchB < 15 {
		t.Fatalf("second batch started without pause: %dms, %dms", secondBatchA, secondBatchB)
	}
}

func TestRunBatchSkipsGroupingWhenBatchSizeCoversInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	results, err := RunBatch(ctx, []int{1, 2, 3}, func(_ context.Context, item int) (int, error) {
		return item * 2, nil
	}, 2, 100*time.Millisecond, 3)
	if err != nil {
		t.Fatalf("RunBatch returned error: %v", err)
	}
	if !reflect.DeepEqual(results, []int{2, 4, 6}) {
		t.Fatalf("RunBatch results = %#v, want input order", results)
	}
}

func TestRunBatchReturnsFirstHandlerError(t *testing.T) {
	want := errors.New("boom")
	_, err := RunBatch(context.Background(), []int{1, 2, 3}, func(_ context.Context, item int) (int, error) {
		if item == 2 {
			return 0, want
		}
		return item, nil
	}, 2, 0, 0)
	if !errors.Is(err, want) {
		t.Fatalf("RunBatch error = %v, want %v", err, want)
	}
}
