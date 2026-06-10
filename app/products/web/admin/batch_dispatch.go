package admin

import (
	"context"
	"net/http"
	"sync"
	"time"

	runtimepkg "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func adminBatchDispatch(w http.ResponseWriter, tokens []string, handler adminBatchHandler, options adminBatchOptions) {
	if options.Async {
		payload := adminBatchDispatchAsync(tokens, handler, options.Concurrency)
		writeAdminJSON(w, http.StatusOK, payload)
		return
	}
	payload, err := adminBatchDispatchSync(tokens, handler, options.Concurrency)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, payload)
}

func adminBatchDispatchSync(tokens []string, handler adminBatchHandler, concurrency int) (map[string]any, error) {
	wrapped := func(ctx context.Context, token string) (adminBatchItemResult, error) {
		data, err := handler(ctx, token)
		if err != nil {
			return adminBatchItemResult{Token: token, Error: err.Error()}, nil
		}
		return adminBatchItemResult{Token: token, Data: data}, nil
	}
	results, err := runtimepkg.RunBatch(context.Background(), tokens, wrapped, concurrency, 0, 0)
	if err != nil {
		return nil, err
	}
	return adminBatchResultPayload(tokens, results), nil
}

func adminBatchDispatchAsync(tokens []string, handler adminBatchHandler, concurrency int) map[string]any {
	task := runtimepkg.CreateTask(len(tokens))
	adminBatchAsyncRunner(func() {
		runAdminBatchTask(task, tokens, handler, concurrency)
	})
	return map[string]any{"status": "success", "task_id": task.ID, "total": len(tokens)}
}

func runAdminBatchTask(task *runtimepkg.AsyncTask, tokens []string, handler adminBatchHandler, concurrency int) {
	defer func() {
		go runtimepkg.ExpireTask(context.Background(), task.ID, 300*time.Second)
	}()
	results := make([]adminBatchItemResult, len(tokens))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxAdminBatchInt(1, concurrency))
	for i, token := range tokens {
		if task.Cancelled {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go runAdminBatchTaskItem(task, handler, token, &results[i], sem, &wg)
	}
	wg.Wait()
	finishAdminBatchTask(task, tokens, results)
}

func runAdminBatchTaskItem(task *runtimepkg.AsyncTask, handler adminBatchHandler, token string, out *adminBatchItemResult, sem chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { <-sem }()
	if task.Cancelled {
		return
	}
	data, err := handler(context.Background(), token)
	*out = adminBatchItemResult{Token: token, Data: data}
	if err != nil {
		out.Error = err.Error()
	}
	adminBatchRecordTaskItem(task, *out)
}

func adminBatchRecordTaskItem(task *runtimepkg.AsyncTask, item adminBatchItemResult) {
	if item.Error != "" {
		task.Record(false, runtimepkg.TaskRecordOptions{Item: adminAssetMask(item.Token), Error: item.Error})
		return
	}
	task.Record(true, runtimepkg.TaskRecordOptions{Item: adminAssetMask(item.Token), Detail: item.Data})
}

func finishAdminBatchTask(task *runtimepkg.AsyncTask, tokens []string, results []adminBatchItemResult) {
	if task.Cancelled {
		task.FinishCancelled()
		return
	}
	task.Finish(adminBatchResultPayload(tokens, results))
}

func adminBatchResultPayload(tokens []string, raw []adminBatchItemResult) map[string]any {
	results := map[string]any{}
	okCount, failCount := 0, 0
	for _, item := range raw {
		if item.Token == "" {
			continue
		}
		if item.Error == "" {
			okCount++
			results[adminAssetMask(item.Token)] = item.Data
			continue
		}
		failCount++
		results[adminAssetMask(item.Token)] = map[string]any{"error": item.Error}
	}
	return map[string]any{
		"status":  "success",
		"summary": map[string]any{"total": len(tokens), "ok": okCount, "fail": failCount},
		"results": results,
	}
}
