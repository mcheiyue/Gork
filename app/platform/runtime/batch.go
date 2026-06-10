package runtime

import (
	"context"
	"sync"
	"time"
)

// RunBatch processes items with bounded concurrency and returns results in input order.
func RunBatch[T any, R any](
	ctx context.Context,
	items []T,
	handler func(context.Context, T) (R, error),
	concurrency int,
	pause time.Duration,
	batchSize int,
) ([]R, error) {
	results := make([]R, len(items))
	if len(items) == 0 {
		return results, nil
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if batchSize <= 0 || batchSize >= len(items) {
		return runBatchChunk(ctx, items, results, 0, handler, concurrency)
	}

	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		if _, err := runBatchChunk(ctx, items[start:end], results, start, handler, concurrency); err != nil {
			return results, err
		}
		if pause > 0 && end < len(items) {
			timer := time.NewTimer(pause)
			select {
			case <-ctx.Done():
				timer.Stop()
				return results, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return results, nil
}

func runBatchChunk[T any, R any](
	ctx context.Context,
	items []T,
	results []R,
	offset int,
	handler func(context.Context, T) (R, error),
	concurrency int,
) ([]R, error) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}

	for i, item := range items {
		select {
		case <-ctx.Done():
			recordErr(ctx.Err())
			continue
		case sem <- struct{}{}:
		}

		wg.Add(1)
		go func(index int, value T) {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := handler(ctx, value)
			if err != nil {
				recordErr(err)
				return
			}
			results[offset+index] = result
		}(i, item)
	}
	wg.Wait()
	return results, firstErr
}
