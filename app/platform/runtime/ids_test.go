package runtime

import (
	"strconv"
	"sync"
	"testing"
)

func TestNextIDReturnsMonotonicProcessLocalIntegers(t *testing.T) {
	first := NextID()
	second := NextID()

	if first <= 0 {
		t.Fatalf("NextID() = %d, want positive id", first)
	}
	if second != first+1 {
		t.Fatalf("second NextID() = %d, want %d", second, first+1)
	}
}

func TestNextHexFormatsZeroPaddedCounter(t *testing.T) {
	defaultHex := NextHex()
	if len(defaultHex) != 12 {
		t.Fatalf("len(NextHex()) = %d, want 12", len(defaultHex))
	}
	if _, err := strconv.ParseInt(defaultHex, 16, 64); err != nil {
		t.Fatalf("NextHex() = %q, want lowercase hex: %v", defaultHex, err)
	}

	shortHex := NextHex(4)
	if len(shortHex) != 4 {
		t.Fatalf("len(NextHex(4)) = %d, want 4", len(shortHex))
	}
	if _, err := strconv.ParseInt(shortHex, 16, 64); err != nil {
		t.Fatalf("NextHex(4) = %q, want lowercase hex: %v", shortHex, err)
	}
}

func TestNextHexUsesSharedMonotonicCounter(t *testing.T) {
	start := idCounter.Load()
	got := NextHex()
	after := idCounter.Load()
	if after != start+1 {
		t.Fatalf("counter after NextHex() = %d, want %d", after, start+1)
	}

	parsed, err := strconv.ParseInt(got, 16, 64)
	if err != nil {
		t.Fatalf("NextHex() = %q, want parseable hex: %v", got, err)
	}
	if parsed != after {
		t.Fatalf("NextHex() parsed = %d, want counter value %d", parsed, after)
	}
}

func TestNextHexRejectsNegativeWidth(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NextHex(-1) should panic")
		}
	}()

	_ = NextHex(-1)
}

func TestNextIDIsConcurrentSafe(t *testing.T) {
	const workers = 64
	const perWorker = 32

	var wg sync.WaitGroup
	ids := make(chan int64, workers*perWorker)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				ids <- NextID()
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[int64]struct{}, workers*perWorker)
	for id := range ids {
		if id <= 0 {
			t.Fatalf("NextID() returned non-positive id %d", id)
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("NextID() returned duplicate id %d", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("got %d unique ids, want %d", len(seen), workers*perWorker)
	}
}
