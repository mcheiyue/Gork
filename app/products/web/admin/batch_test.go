package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/platform"
	runtimepkg "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func TestAdminBatchNSFWSyncUsesListedTokensAndPatchesTags(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{pages: []adminAssetsListResult{{
		Total: 4,
		Items: []adminAssetsAccount{
			{Token: "tok-active", Status: "active"},
			{Token: "tok-cooling", Status: "cooling"},
			{Token: "tok-disabled", Status: "disabled"},
			{Token: "tok-deleted", Status: "active", Deleted: true},
		},
	}}}
	adminBatchRepoProvider = func() adminBatchRepository { return repo }
	var sequence []string
	adminBatchNSFWSequence = func(_ context.Context, token string) error {
		sequence = append(sequence, token)
		return nil
	}

	rec := adminRequest(http.MethodPost, "/admin/api/batch/nsfw?enabled=true&concurrency=1", `{"tokens":[]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	summary := body["summary"].(map[string]any)
	if body["status"] != "success" || int(summary["total"].(float64)) != 2 || int(summary["ok"].(float64)) != 2 {
		t.Fatalf("body = %#v", body)
	}
	if len(sequence) != 2 || sequence[0] != "tok-active" || sequence[1] != "tok-cooling" {
		t.Fatalf("sequence = %#v", sequence)
	}
	if len(repo.patches) != 2 || repo.patches[0].AddTags[0] != "nsfw" || repo.patches[1].Token != "tok-cooling" {
		t.Fatalf("patches = %#v", repo.patches)
	}
}

func TestAdminBatchNSFWDoesNotPatchTagsWhenUpstreamFails(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminBatchRepoProvider = func() adminBatchRepository { return repo }
	adminBatchNSFWSequence = func(_ context.Context, token string) error {
		if token != "tok-fail" {
			t.Fatalf("token = %q", token)
		}
		return platform.NewUpstreamError("Upstream returned 403", http.StatusForbidden, "")
	}

	rec := adminRequest(http.MethodPost, "/admin/api/batch/nsfw?enabled=true&concurrency=1", `{"tokens":["tok-fail"]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	summary := body["summary"].(map[string]any)
	results := body["results"].(map[string]any)
	result := results["tok-fail"].(map[string]any)
	if int(summary["ok"].(float64)) != 0 || int(summary["fail"].(float64)) != 1 {
		t.Fatalf("body = %#v", body)
	}
	if !strings.Contains(result["error"].(string), "Upstream returned 403") {
		t.Fatalf("result = %#v", result)
	}
	if len(repo.patches) != 0 {
		t.Fatalf("patches = %#v, want no local tag update after upstream failure", repo.patches)
	}
}

func TestAdminBatchNSFWDisableCallsUpstreamBeforeRemovingTags(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminBatchRepoProvider = func() adminBatchRepository { return repo }
	var setCalls []struct {
		token   string
		enabled bool
	}
	adminBatchSetNSFW = func(_ context.Context, token string, enabled bool) error {
		setCalls = append(setCalls, struct {
			token   string
			enabled bool
		}{token: token, enabled: enabled})
		return nil
	}

	longToken := "1234567890abcdefghijklmnopqrstuvwxyz"
	rec := adminRequest(http.MethodPost, "/admin/api/batch/nsfw?enabled=false&concurrency=1", `{"tokens":["`+longToken+`"]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	results := body["results"].(map[string]any)
	result := results["12345678...stuvwxyz"].(map[string]any)
	if result["tagged"] != false || len(setCalls) != 1 || setCalls[0].token != longToken || setCalls[0].enabled {
		t.Fatalf("body=%#v setCalls=%#v", body, setCalls)
	}
	if len(repo.patches) != 1 || repo.patches[0].Token != longToken || repo.patches[0].RemoveTags[0] != "nsfw" {
		t.Fatalf("patches = %#v", repo.patches)
	}
}

func TestAdminBatchNSFWNoTokensAvailableReturnsValidation(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminBatchRepoProvider = func() adminBatchRepository { return repo }

	rec := adminRequest(http.MethodPost, "/admin/api/batch/nsfw", `{"tokens":[]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status/body=%d/%#v", rec.Code, body)
	}
	errBody := body["error"].(map[string]any)
	if errBody["message"] != "No tokens available" || errBody["param"] != "tokens" {
		t.Fatalf("error body = %#v", errBody)
	}
}

func TestAdminBatchRefreshSyncRecordsFailures(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	adminBatchRefreshServiceProvider = func() adminBatchRefreshService {
		return fakeAdminBatchRefreshService{refreshed: map[string]int{"tok-ok": 1}}
	}

	rec := adminRequest(http.MethodPost, "/admin/api/batch/refresh", `{"tokens":["tok-ok","tok-bad"]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	summary := body["summary"].(map[string]any)
	if int(summary["ok"].(float64)) != 1 || int(summary["fail"].(float64)) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	results := body["results"].(map[string]any)
	if results["tok-ok"].(map[string]any)["refreshed"].(float64) != 1 {
		t.Fatalf("tok-ok result = %#v", results["tok-ok"])
	}
	errText := results["tok-bad"].(map[string]any)["error"].(string)
	if !strings.Contains(errText, "未获取到真实配额数据") {
		t.Fatalf("tok-bad error = %q", errText)
	}
}

func TestAdminBatchConcurrencyUsesQueryAndConfigFallback(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	adminBatchConfigInt = func(key string, fallback int) int {
		if key != "batch.refresh_concurrency" || fallback != 50 {
			t.Fatalf("config key/fallback=%s/%d", key, fallback)
		}
		return 0
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/api/batch/refresh", nil)
	value, err := adminBatchConcurrency(req, "batch.refresh_concurrency")
	if err != nil || value != 1 {
		t.Fatalf("fallback value/err=%d/%v", value, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/api/batch/refresh?concurrency=7", nil)
	value, err = adminBatchConcurrency(req, "batch.refresh_concurrency")
	if err != nil || value != 7 {
		t.Fatalf("override value/err=%d/%v", value, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/api/batch/refresh?concurrency=0", nil)
	if _, err = adminBatchConcurrency(req, "batch.refresh_concurrency"); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestAdminBatchCacheClearUsesAssetDeletionSource(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"items": []any{
			map[string]any{"assetId": "asset-1"},
			map[string]any{"name": "missing-id"},
		}}, nil
	}
	var deleted []string
	adminDeleteAsset = func(_ context.Context, token string, assetID string) error {
		deleted = append(deleted, token+"|"+assetID)
		return nil
	}

	rec := adminRequest(http.MethodPost, "/admin/api/batch/cache-clear", `{"tokens":["tok"]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	result := body["results"].(map[string]any)["tok"].(map[string]any)
	if int(result["deleted"].(float64)) != 1 || len(deleted) != 1 || deleted[0] != "tok|asset-1" {
		t.Fatalf("body=%#v deleted=%#v", body, deleted)
	}
}

func TestAdminBatchCacheClearContinuesUntilInvalidCredentialMarked(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"assets": []any{
			map[string]any{"id": "ok-1"},
			map[string]any{"id": "temporary"},
			map[string]any{"id": "invalid"},
			map[string]any{"id": "ok-2"},
		}}, nil
	}
	var attempts []string
	adminDeleteAsset = func(_ context.Context, token string, assetID string) error {
		attempts = append(attempts, token+"|"+assetID)
		switch assetID {
		case "temporary":
			return errors.New("temporary failure")
		case "invalid":
			return errors.New("invalid credentials")
		default:
			return nil
		}
	}
	var marked []string
	adminMarkInvalidCredentials = func(_ context.Context, _ adminAssetsRepository, token string, err error, source string) bool {
		marked = append(marked, token+"|"+err.Error()+"|"+source)
		return err.Error() == "invalid credentials"
	}

	rec := adminRequest(http.MethodPost, "/admin/api/batch/cache-clear", `{"tokens":["tok"]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	summary := body["summary"].(map[string]any)
	result := body["results"].(map[string]any)["tok"].(map[string]any)
	if int(summary["fail"].(float64)) != 1 || !strings.Contains(result["error"].(string), "invalid credentials") {
		t.Fatalf("body = %#v", body)
	}
	if len(attempts) != 4 || attempts[0] != "tok|ok-1" || attempts[3] != "tok|ok-2" {
		t.Fatalf("attempts = %#v", attempts)
	}
	if len(marked) != 2 || marked[0] != "tok|temporary failure|asset batch clear" || marked[1] != "tok|invalid credentials|asset batch clear" {
		t.Fatalf("marked = %#v", marked)
	}
}

func TestAdminBatchCacheClearNoTokensAvailableReturnsValidation(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }

	rec := adminRequest(http.MethodPost, "/admin/api/batch/cache-clear", `{"tokens":[]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status/body=%d/%#v", rec.Code, body)
	}
	errBody := body["error"].(map[string]any)
	if errBody["message"] != "No tokens available" || errBody["param"] != "tokens" {
		t.Fatalf("error body = %#v", errBody)
	}
}

func TestAdminBatchAsyncCreatesTaskAndSSEFinal(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{}
	adminBatchRepoProvider = func() adminBatchRepository { return repo }
	adminBatchAsyncRunner = func(run func()) { run() }
	adminBatchSetNSFW = func(context.Context, string, bool) error { return nil }

	rec := adminRequest(http.MethodPost, "/admin/api/batch/nsfw?async=true&enabled=false", `{"tokens":["tok"]}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	taskID := body["task_id"].(string)
	if taskID == "" || int(body["total"].(float64)) != 1 {
		t.Fatalf("async body = %#v", body)
	}

	stream := adminRequest(http.MethodGet, "/admin/api/batch/"+taskID+"/stream?app_key=grok2api", "", "")
	text := stream.Body.String()
	if stream.Code != http.StatusOK {
		t.Fatalf("stream status/body=%d/%s", stream.Code, text)
	}
	if !strings.Contains(text, `"type":"snapshot"`) || !strings.Contains(text, `"type":"done"`) {
		t.Fatalf("stream body = %q", text)
	}
}

func TestAdminBatchStreamAndCancelMissingTaskReturnNotFound(t *testing.T) {
	resetAdminRouterDepsForTest(t)

	stream := adminRequest(http.MethodGet, "/admin/api/batch/missing/stream", "", "Bearer grok2api")
	streamBody := decodeAdminBody(t, stream)
	if stream.Code != http.StatusNotFound || streamBody["error"].(map[string]any)["code"] != "task_not_found" {
		t.Fatalf("stream status/body=%d/%#v", stream.Code, streamBody)
	}

	cancel := adminRequest(http.MethodPost, "/admin/api/batch/missing/cancel", "", "Bearer grok2api")
	cancelBody := decodeAdminBody(t, cancel)
	if cancel.Code != http.StatusNotFound || cancelBody["error"].(map[string]any)["code"] != "task_not_found" {
		t.Fatalf("cancel status/body=%d/%#v", cancel.Code, cancelBody)
	}
}

func TestAdminBatchCancelMarksTaskCancelled(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	task := runtimepkg.CreateTask(1)
	rec := adminRequest(http.MethodPost, "/admin/api/batch/"+task.ID+"/cancel", "", "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if body["status"] != "success" || !task.Cancelled {
		t.Fatalf("body=%#v cancelled=%v", body, task.Cancelled)
	}
}

func TestAdminBatchRouteGoldenStatusHeadersAndShapes(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminBatchRepo{pages: []adminAssetsListResult{{
		Total: 1,
		Items: []adminAssetsAccount{{Token: "tok-listed", Status: "active"}},
	}}}
	adminBatchRepoProvider = func() adminBatchRepository { return repo }
	adminBatchConfigInt = func(string, int) int { return 1 }
	adminBatchNSFWSequence = func(context.Context, string) error { return nil }
	adminBatchSetNSFW = func(context.Context, string, bool) error { return nil }
	adminBatchAsyncRunner = func(run func()) { run() }
	adminBatchRefreshServiceProvider = func() adminBatchRefreshService {
		return fakeAdminBatchRefreshService{refreshed: map[string]int{"tok-refresh": 2}}
	}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"items": []any{map[string]any{"assetId": "asset-1"}}}, nil
	}
	adminDeleteAsset = func(context.Context, string, string) error { return nil }

	for _, tt := range []struct {
		name   string
		path   string
		body   string
		status int
		json   map[string]any
	}{
		{
			name:   "nsfw sync",
			path:   "/admin/api/batch/nsfw?enabled=true&concurrency=1",
			body:   `{"tokens":["tok-nsfw"]}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":                  "success",
				"summary.total":           float64(1),
				"summary.ok":              float64(1),
				"summary.fail":            float64(0),
				"results.tok-nsfw.tagged": true,
			},
		},
		{
			name:   "refresh sync",
			path:   "/admin/api/batch/refresh?concurrency=1",
			body:   `{"tokens":["tok-refresh"]}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":                        "success",
				"summary.total":                 float64(1),
				"summary.ok":                    float64(1),
				"summary.fail":                  float64(0),
				"results.tok-refresh.refreshed": float64(2),
			},
		},
		{
			name:   "cache clear sync",
			path:   "/admin/api/batch/cache-clear?concurrency=1",
			body:   `{"tokens":["tok-cache"]}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":                    "success",
				"summary.total":             float64(1),
				"summary.ok":                float64(1),
				"summary.fail":              float64(0),
				"results.tok-cache.deleted": float64(1),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := adminRequest(http.MethodPost, tt.path, tt.body, "Bearer grok2api")
			assertAdminGoldenJSON(t, rec, tt.status, tt.json)
		})
	}

	async := adminRequest(http.MethodPost, "/admin/api/batch/nsfw?async=true&enabled=false&concurrency=1", `{"tokens":["tok-async"]}`, "Bearer grok2api")
	assertAdminGoldenJSON(t, async, http.StatusOK, map[string]any{"status": "success", "total": float64(1)})
	asyncBody := decodeAdminBody(t, async)
	taskID, _ := asyncBody["task_id"].(string)
	if taskID == "" {
		t.Fatalf("async task_id missing in %#v", asyncBody)
	}
	stream := adminRequest(http.MethodGet, "/admin/api/batch/"+taskID+"/stream", "", "Bearer grok2api")
	if stream.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", stream.Code, stream.Body.String())
	}
	if got := stream.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("stream content-type=%q", got)
	}
	streamBody := stream.Body.String()
	if !strings.Contains(streamBody, "data: ") || !strings.Contains(streamBody, `"type":"snapshot"`) || !strings.Contains(streamBody, `"type":"done"`) {
		t.Fatalf("stream body = %q", streamBody)
	}

	task := runtimepkg.CreateTask(1)
	cancel := adminRequest(http.MethodPost, "/admin/api/batch/"+task.ID+"/cancel", "", "Bearer grok2api")
	assertAdminGoldenJSON(t, cancel, http.StatusOK, map[string]any{"status": "success"})
	if !task.Cancelled {
		t.Fatalf("task not cancelled")
	}

	methodGuard := adminRequest(http.MethodGet, "/admin/api/batch/nsfw", "", "Bearer grok2api")
	assertAdminGoldenJSON(t, methodGuard, http.StatusMethodNotAllowed, map[string]any{"error.message": "Method not allowed"})

	matrix := []struct {
		planPath string
		covered  bool
	}{
		{planPath: "POST /admin/api/batch/nsfw", covered: true},
		{planPath: "POST /admin/api/batch/refresh", covered: true},
		{planPath: "POST /admin/api/batch/cache-clear", covered: true},
		{planPath: "GET /admin/api/batch/{task_id}/stream", covered: true},
		{planPath: "POST /admin/api/batch/{task_id}/cancel", covered: true},
	}
	gaps := 0
	for _, row := range matrix {
		if !row.covered {
			t.Errorf("missing golden coverage for %s", row.planPath)
			gaps++
		}
	}
	t.Logf("admin_batch_route_golden_matrix rows=%d gaps=%d", len(matrix), gaps)
}

type fakeAdminBatchRepo struct {
	pages   []adminAssetsListResult
	patches []adminBatchAccountPatch
}

func (r *fakeAdminBatchRepo) ListAccounts(_ context.Context, query adminAssetsListQuery) (adminAssetsListResult, error) {
	if query.Page <= 0 || query.Page > len(r.pages) {
		return adminAssetsListResult{TotalPages: 1}, nil
	}
	return r.pages[query.Page-1], nil
}

func (r *fakeAdminBatchRepo) PatchAccounts(_ context.Context, patches []adminBatchAccountPatch) (adminTokensPatchResult, error) {
	r.patches = append(r.patches, patches...)
	return adminTokensPatchResult{Patched: len(patches)}, nil
}

type fakeAdminBatchRefreshService struct {
	refreshed map[string]int
}

func (s fakeAdminBatchRefreshService) RefreshTokens(_ context.Context, tokens []string) (adminBatchRefreshResult, error) {
	return adminBatchRefreshResult{Refreshed: s.refreshed[tokens[0]]}, nil
}
