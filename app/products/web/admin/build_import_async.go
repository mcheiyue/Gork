package admin

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

const buildImportChunkSize = 100

var adminBuildAsyncRunner = func(run func()) { go run() }

// handleAdminBuildAccountsImportAsync 分片异步导入，进度走 /admin/api/batch/{id}/stream。
func handleAdminBuildAccountsImportAsync(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	// 分片导入允许更大 body（约 32MB）
	raw, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	creds, err := build.ParseCredentials(raw)
	if err != nil {
		writeAdminError(w, platform.NewValidationError(err.Error(), "body", "invalid_credentials"))
		return
	}
	if len(creds) == 0 {
		writeAdminError(w, platform.NewValidationError("账号凭据中没有账号", "body", "empty_accounts"))
		return
	}
	task := runtimepkg.CreateTask(len(creds))
	adminBuildAsyncRunner(func() {
		runBuildAccountsImportTask(task.Context(), store, task, creds)
	})
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"task_id": task.ID,
		"total":   len(creds),
	})
}

func runBuildAccountsImportTask(ctx context.Context, store buildAccountAdminStore, task *runtimepkg.AsyncTask, creds []build.Credential) {
	defer goExpireBuildTask(task.ID)
	saved, failed := 0, 0
	for start := 0; start < len(creds); start += buildImportChunkSize {
		if task.IsCancelled() {
			task.FinishCancelled()
			return
		}
		end := start + buildImportChunkSize
		if end > len(creds) {
			end = len(creds)
		}
		chunkOK, chunkFail := 0, 0
		for _, cred := range creds[start:end] {
			if task.IsCancelled() {
				task.FinishCancelled()
				return
			}
			if _, err := store.Upsert(ctx, buildaccount.FromCredential(cred)); err != nil {
				chunkFail++
				failed++
				task.Record(false, runtimepkg.TaskRecordOptions{
					Count: 1,
					Error: err.Error(),
					Detail: map[string]any{
						"name":    cred.Name,
						"user_id": cred.UserID,
					},
				})
				continue
			}
			chunkOK++
			saved++
			task.Record(true, runtimepkg.TaskRecordOptions{
				Count: 1,
				Detail: map[string]any{
					"saved":  saved,
					"failed": failed,
				},
			})
		}
		_ = chunkOK
		_ = chunkFail
	}
	task.Finish(map[string]any{
		"status":  "success",
		"summary": map[string]any{"total": len(creds), "ok": saved, "saved": saved, "fail": failed},
	})
}

func goExpireBuildTask(taskID string) {
	go func() {
		_ = runtimepkg.ExpireTask(context.Background(), taskID, 300*time.Second)
	}()
}
