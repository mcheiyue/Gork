package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/platform"
	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

type buildBatchRequest struct {
	IDs    []int64 `json:"ids"`
	All    bool    `json:"all"`
	Status string  `json:"status"`
	Mode   string  `json:"mode"` // cleanup: expired|invalid|disabled
}

// handleAdminBuildAccountsBillingBatch 批量同步 Billing（异步任务）。
func handleAdminBuildAccountsBillingBatch(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, err := decodeBuildBatchRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	targets, err := resolveBuildBatchTargets(r.Context(), store, req)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	if len(targets) == 0 {
		writeAdminError(w, platform.NewValidationError("no matching build accounts", "ids", ""))
		return
	}
	task := runtimepkg.CreateTask(len(targets))
	adminBuildAsyncRunner(func() {
		runBuildBillingBatch(task.Context(), store, task, targets)
	})
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "task_id": task.ID, "total": len(targets)})
}

// handleAdminBuildAccountsRefreshBatch 批量 OAuth refresh（异步）。
func handleAdminBuildAccountsRefreshBatch(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, err := decodeBuildBatchRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	targets, err := resolveBuildBatchTargets(r.Context(), store, req)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	if len(targets) == 0 {
		writeAdminError(w, platform.NewValidationError("no matching build accounts", "ids", ""))
		return
	}
	task := runtimepkg.CreateTask(len(targets))
	adminBuildAsyncRunner(func() {
		runBuildRefreshBatch(task.Context(), store, task, targets)
	})
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "task_id": task.ID, "total": len(targets)})
}

// handleAdminBuildAccountsCleanup 清理过期/无效/已禁用账号（异步软删）。
func handleAdminBuildAccountsCleanup(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, err := decodeBuildBatchRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "expired"
	}
	switch mode {
	case "expired", "invalid", "disabled":
	default:
		writeAdminError(w, platform.NewValidationError("mode must be expired|invalid|disabled", "mode", ""))
		return
	}
	accounts, err := store.List(r.Context())
	if err != nil {
		writeAdminError(w, err)
		return
	}
	now := time.Now().UTC()
	var targets []buildaccount.Account
	for _, acc := range accounts {
		if matchBuildCleanup(acc, mode, now) {
			targets = append(targets, acc)
		}
	}
	if len(targets) == 0 {
		writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "task_id": "", "total": 0, "deleted": 0})
		return
	}
	task := runtimepkg.CreateTask(len(targets))
	adminBuildAsyncRunner(func() {
		runBuildCleanupBatch(task.Context(), store, task, targets, mode)
	})
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "task_id": task.ID, "total": len(targets)})
}

func decodeBuildBatchRequest(r *http.Request) (buildBatchRequest, error) {
	var req buildBatchRequest
	if r.Body == nil {
		return req, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if err.Error() == "EOF" {
			return req, nil
		}
		return req, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	return req, nil
}

func resolveBuildBatchTargets(ctx context.Context, store buildAccountAdminStore, req buildBatchRequest) ([]buildaccount.Account, error) {
	accounts, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	wantStatus := strings.ToLower(strings.TrimSpace(req.Status))
	if len(req.IDs) > 0 {
		idSet := make(map[int64]struct{}, len(req.IDs))
		for _, id := range req.IDs {
			if id > 0 {
				idSet[id] = struct{}{}
			}
		}
		var out []buildaccount.Account
		for _, acc := range accounts {
			if _, ok := idSet[acc.ID]; !ok {
				continue
			}
			if wantStatus != "" && !strings.EqualFold(acc.Status, wantStatus) {
				continue
			}
			out = append(out, acc)
		}
		return out, nil
	}
	var out []buildaccount.Account
	for _, acc := range accounts {
		if wantStatus != "" {
			if strings.EqualFold(acc.Status, wantStatus) {
				out = append(out, acc)
			}
			continue
		}
		if req.All || acc.Status == buildaccount.StatusActive {
			out = append(out, acc)
		}
	}
	return out, nil
}

func matchBuildCleanup(acc buildaccount.Account, mode string, now time.Time) bool {
	switch mode {
	case "expired":
		return isBuildTokenExpired(acc, now) || acc.Status == buildaccount.StatusExpired
	case "invalid":
		return acc.AccessToken == "" && acc.RefreshToken == ""
	case "disabled":
		return acc.Status == buildaccount.StatusDisabled
	default:
		return false
	}
}
