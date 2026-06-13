package admin

import (
	"context"
	"time"

	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

func adminTokensRunAddImport(ctx context.Context, repo adminTokensRepository, refresh adminTokensRefreshService, task *runtimepkg.AsyncTask, pool string, tokens []string, tags []string, autoNSFW bool) {
	defer adminTokensExpireTask(task)
	saved, skipped := 0, 0
	refreshTokens := []string{}
	for start := 0; start < len(tokens); start += adminTokensImportChunkSize {
		if task.Cancelled {
			task.FinishCancelled()
			return
		}
		chunk := adminTokensChunk(tokens, start)
		newTokens, err := adminTokensImportNewTokens(ctx, repo, chunk)
		if err != nil {
			task.FailTask(err.Error())
			return
		}
		skipped += len(chunk) - len(newTokens)
		if len(newTokens) > 0 {
			result, err := repo.UpsertAccounts(ctx, adminTokensUpserts(newTokens, pool, tags))
			if err != nil {
				task.FailTask(err.Error())
				return
			}
			saved += adminTokensUpserted(result, len(newTokens))
			refreshTokens = append(refreshTokens, newTokens...)
		}
		task.Record(true, runtimepkg.TaskRecordOptions{Count: len(chunk), Detail: map[string]any{"saved": saved, "skipped": skipped}})
	}
	adminTokensFinishImport(ctx, repo, refresh, task, refreshTokens, autoNSFW, map[string]any{"mode": "add", "total": len(tokens), "ok": saved, "saved": saved, "fail": 0, "skipped": skipped})
}

func adminTokensRunReplaceImport(ctx context.Context, repo adminTokensRepository, refresh adminTokensRefreshService, task *runtimepkg.AsyncTask, payload map[string][]adminTokensUpsert, autoNSFW bool) {
	defer adminTokensExpireTask(task)
	saved := 0
	refreshTokens := []string{}
	for pool, upserts := range payload {
		if task.Cancelled {
			task.FinishCancelled()
			return
		}
		result, err := repo.ReplacePool(ctx, adminTokensReplacePoolCommand{Pool: pool, Upserts: upserts})
		if err != nil {
			task.FailTask(err.Error())
			return
		}
		saved += adminTokensUpserted(result, len(upserts))
		for _, upsert := range upserts {
			refreshTokens = append(refreshTokens, upsert.Token)
		}
		task.Record(true, runtimepkg.TaskRecordOptions{Count: len(upserts), Detail: map[string]any{"pool": pool, "saved": saved}})
	}
	adminTokensFinishImport(ctx, repo, refresh, task, refreshTokens, autoNSFW, map[string]any{"mode": "replace", "total": len(refreshTokens), "ok": saved, "saved": saved, "fail": 0, "skipped": 0})
}

func adminTokensImportNewTokens(ctx context.Context, repo adminTokensRepository, chunk []string) ([]string, error) {
	records, err := repo.GetAccounts(ctx, chunk)
	if err != nil {
		return nil, err
	}
	return adminTokensNewOnly(chunk, records), nil
}

func adminTokensFinishImport(ctx context.Context, repo adminTokensRepository, refresh adminTokensRefreshService, task *runtimepkg.AsyncTask, tokens []string, autoNSFW bool, result map[string]any) {
	if len(tokens) > 0 {
		if _, err := refresh.RefreshOnImport(ctx, tokens); err != nil {
			task.FailTask(err.Error())
			return
		}
	}
	if autoNSFW {
		count, err := adminTokensEnableAutoNSFW(ctx, repo, tokens)
		if err != nil {
			task.FailTask(err.Error())
			return
		}
		result["nsfw"] = count
	}
	task.Finish(map[string]any{"status": "success", "summary": result})
}

func adminTokensEnableAutoNSFW(ctx context.Context, repo adminTokensRepository, tokens []string) (int, error) {
	if len(tokens) == 0 {
		return 0, nil
	}
	records, err := repo.GetAccounts(ctx, tokens)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, record := range records {
		if !adminAssetAccountManageable(record) {
			continue
		}
		if _, err := adminBatchNSFWOne(ctx, repo, record.Token, true); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func adminTokensChunk(tokens []string, start int) []string {
	end := start + adminTokensImportChunkSize
	if end > len(tokens) {
		end = len(tokens)
	}
	return tokens[start:end]
}

func adminTokensExpireTask(task *runtimepkg.AsyncTask) {
	go runtimepkg.ExpireTask(context.Background(), task.ID, 300*time.Second)
}
