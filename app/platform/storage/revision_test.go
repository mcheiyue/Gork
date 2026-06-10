package storage

import (
	"sync"
	"testing"
)

func TestRevisionTrackerCurrentBumpAndSet(t *testing.T) {
	tracker := NewRevisionTracker(7)
	if got := tracker.Current(); got != 7 {
		t.Fatalf("Current() = %d, want 7", got)
	}
	if got := tracker.Bump(); got != 8 {
		t.Fatalf("Bump() = %d, want 8", got)
	}
	if got := tracker.Current(); got != 8 {
		t.Fatalf("Current() after Bump = %d, want 8", got)
	}

	tracker.Set(42)
	if got := tracker.Current(); got != 42 {
		t.Fatalf("Current() after Set = %d, want 42", got)
	}
}

func TestRevisionTrackerBumpIsConcurrentSafe(t *testing.T) {
	tracker := NewRevisionTracker(0)
	const workers = 32
	const perWorker = 64

	var wg sync.WaitGroup
	values := make(chan int64, workers*perWorker)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				values <- tracker.Bump()
			}
		}()
	}
	wg.Wait()
	close(values)

	seen := map[int64]struct{}{}
	for value := range values {
		if _, ok := seen[value]; ok {
			t.Fatalf("duplicate revision %d", value)
		}
		seen[value] = struct{}{}
	}
	want := int64(workers * perWorker)
	if got := tracker.Current(); got != want {
		t.Fatalf("Current() = %d, want %d", got, want)
	}
	if len(seen) != int(want) {
		t.Fatalf("got %d unique revisions, want %d", len(seen), want)
	}
}
