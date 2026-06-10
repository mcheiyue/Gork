package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jiujiu532/grok2api/app/platform"
	runtimepkg "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func handleAdminBatchStream(w http.ResponseWriter, r *http.Request) {
	taskID, ok := adminBatchTaskID(r, "/stream")
	if !ok {
		writeAdminError(w, adminBatchTaskNotFound())
		return
	}
	task := runtimepkg.GetTask(taskID)
	if task == nil {
		writeAdminError(w, adminBatchTaskNotFound())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	streamAdminBatchTask(w, r, task)
}

func handleAdminBatchCancel(w http.ResponseWriter, r *http.Request) {
	taskID, ok := adminBatchTaskID(r, "/cancel")
	if !ok {
		writeAdminError(w, adminBatchTaskNotFound())
		return
	}
	task := runtimepkg.GetTask(taskID)
	if task == nil {
		writeAdminError(w, adminBatchTaskNotFound())
		return
	}
	task.Cancel()
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func streamAdminBatchTask(w http.ResponseWriter, r *http.Request, task *runtimepkg.AsyncTask) {
	queue := task.Attach()
	defer task.Detach(queue)
	adminBatchWriteSSE(w, adminBatchSnapshot(task))
	if final := task.FinalEvent(); final != nil {
		adminBatchWriteSSE(w, final)
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-queue:
			adminBatchWriteSSE(w, event)
			if adminBatchFinalType(event["type"]) {
				return
			}
		case <-time.After(15 * time.Second):
			_, _ = w.Write([]byte(": ping\n\n"))
		}
	}
}

func adminBatchTaskID(r *http.Request, suffix string) (string, bool) {
	tail := strings.TrimPrefix(r.URL.Path, "/admin/api/batch/")
	if !strings.HasSuffix(tail, suffix) {
		return "", false
	}
	taskID := strings.TrimSuffix(tail, suffix)
	taskID = strings.Trim(taskID, "/")
	return taskID, taskID != ""
}

func adminBatchSnapshot(task *runtimepkg.AsyncTask) map[string]any {
	snapshot := task.Snapshot()
	snapshot["type"] = "snapshot"
	return snapshot
}

func adminBatchWriteSSE(w http.ResponseWriter, event map[string]any) {
	raw, _ := json.Marshal(event)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func adminBatchFinalType(value any) bool {
	return value == "done" || value == "error" || value == "cancelled"
}

func adminBatchTaskNotFound() error {
	return platform.NewAppError("Task not found", platform.ErrorKindValidation, "task_not_found", 404, nil)
}
