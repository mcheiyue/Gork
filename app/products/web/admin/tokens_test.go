package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

func TestAdminTokensListSerializesFiltersAndFacets(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{
		listResults: []adminAssetsListResult{{Items: []adminAssetsAccount{{
			Token: "tok-1", Pool: "basic", Status: "active", Tags: []string{"nsfw"},
			Quota:         map[string]any{"auto": map[string]any{"remaining": 2, "total": 5}},
			UsageUseCount: 3, LastUseAt: int64(123),
		}}, Total: 1, Page: 2, PageSize: 10, TotalPages: 1, Revision: 7}},
		facetSnapshot: adminTokensFacetSnapshotFromRecords([]adminAssetsAccount{
			{Token: "tok-1", Pool: "basic", Status: "active", Tags: []string{"nsfw"}, UsageUseCount: 3, UsageFailCount: 1, Quota: map[string]any{"auto": map[string]any{"remaining": 2}}},
			{Token: "tok-2", Pool: "super", Status: "disabled"},
			{Token: "tok-3", Pool: "heavy", Status: "expired"},
		}),
	}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }

	rec := adminRequest(http.MethodGet, "/admin/api/tokens?page=2&page_size=10&pool=basic&status=invalid&nsfw=enabled&sort_by=last_use_at&sort_desc=false", "", "Bearer gork")
	body := decodeAdminBody(t, rec)
	if repo.queries[0].Pool != "basic" || repo.queries[0].Status != "expired" || repo.queries[0].SortDesc {
		t.Fatalf("query = %#v", repo.queries[0])
	}
	if len(repo.queries[0].Tags) != 1 || repo.queries[0].Tags[0] != "nsfw" {
		t.Fatalf("tags filter = %#v", repo.queries[0].Tags)
	}
	tokens := body["tokens"].([]any)
	row := tokens[0].(map[string]any)
	if row["token"] != "tok-1" || row["pool"] != "basic" || row["last_used_at"].(float64) != 123 {
		t.Fatalf("row = %#v", row)
	}
	facets := body["facets"].(map[string]any)
	stats := facets["stats"].(map[string]any)
	if int(stats["calls"].(float64)) != 4 || int(stats["qa"].(float64)) != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if body["revision"].(float64) != 7 {
		t.Fatalf("revision = %#v", body["revision"])
	}
	if len(repo.queries) != 1 || repo.facetCalls != 1 {
		t.Fatalf("list/facet calls = %d/%d", len(repo.queries), repo.facetCalls)
	}
}

func TestAdminTokenMaskMatchesPythonRule(t *testing.T) {
	if got := adminTokenMask("short-token"); got != "short-token" {
		t.Fatalf("short token mask = %q", got)
	}
	got := adminTokenMask("1234567890abcdefghijXYZ987654321")
	if got != "12345678...87654321" {
		t.Fatalf("long token mask = %q", got)
	}
}

func TestAdminTokensSaveAddAndDelete(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{accounts: map[string]adminAssetsAccount{"existing": {Token: "existing", Status: "active"}}}
	refresh := &fakeAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	adminTokensAsyncRunner = func(run func()) { run() }

	rec := adminRequest(http.MethodPost, "/admin/api/tokens", `{"basic":[" sso=tok-a ","tok-b"],"super":[{"token":"tok-c","tags":["x"]}]}`, "Bearer gork")
	body := decodeAdminBody(t, rec)
	if int(body["count"].(float64)) != 3 || len(repo.replaced) != 2 {
		t.Fatalf("save body=%#v replaced=%#v", body, repo.replaced)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/tokens/add", `{"tokens":["existing","new","new"],"pool":"auto","tags":["t"]}`, "Bearer gork")
	body = decodeAdminBody(t, rec)
	if int(body["count"].(float64)) != 1 || int(body["skipped"].(float64)) != 1 || body["synced"] != true {
		t.Fatalf("add body = %#v", body)
	}
	if repo.upserts[len(repo.upserts)-1][0].Token != "new" || refresh.imported[len(refresh.imported)-1][0] != "new" {
		t.Fatalf("upserts=%#v refresh=%#v", repo.upserts, refresh.imported)
	}

	rec = adminRequest(http.MethodDelete, "/admin/api/tokens", `[" new ",""]`, "Bearer gork")
	body = decodeAdminBody(t, rec)
	if int(body["deleted"].(float64)) != 1 || repo.deleted[0][0] != "new" {
		t.Fatalf("delete body=%#v deleted=%#v", body, repo.deleted)
	}
}

func TestAdminTokensEditToggleAndReplacePool(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{accounts: map[string]adminAssetsAccount{
		"old": {Token: "old", Pool: "basic", Status: "active", Tags: []string{"keep"}, Ext: map[string]any{"a": "b"}},
		"one": {Token: "one", Status: "active"},
		"two": {Token: "two", Status: "active"},
	}}
	refresh := &fakeAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	adminTokensAsyncRunner = func(run func()) { run() }
	adminTokensNowMS = func() int64 { return 456 }

	rec := adminRequest(http.MethodPut, "/admin/api/tokens/edit", `{"old_token":"old","token":"new","pool":"super"}`, "Bearer gork")
	body := decodeAdminBody(t, rec)
	if body["token"] != "new" || repo.deleted[0][0] != "old" || repo.upserts[0][0].Token != "new" {
		t.Fatalf("edit body=%#v upserts=%#v deleted=%#v", body, repo.upserts, repo.deleted)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/tokens/disabled", `{"token":"one","disabled":true}`, "Bearer gork")
	body = decodeAdminBody(t, rec)
	if body["disabled"] != true || repo.patches[len(repo.patches)-1][0].Status != "disabled" {
		t.Fatalf("disable body=%#v patches=%#v", body, repo.patches)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/tokens/disabled/batch", `{"tokens":["one","two","one"],"disabled":false}`, "Bearer gork")
	body = decodeAdminBody(t, rec)
	summary := body["summary"].(map[string]any)
	if int(summary["ok"].(float64)) != 2 || repo.patches[len(repo.patches)-1][0].ClearFailures != true {
		t.Fatalf("batch disable body=%#v patches=%#v", body, repo.patches)
	}

	rec = adminRequest(http.MethodPut, "/admin/api/tokens/pool", `{"pool":"heavy","tokens":["one","two"],"tags":["pool-tag"]}`, "Bearer gork")
	body = decodeAdminBody(t, rec)
	if body["pool"] != "heavy" || int(body["count"].(float64)) != 2 || repo.replaced[len(repo.replaced)-1].Pool != "heavy" {
		t.Fatalf("pool body=%#v replaced=%#v", body, repo.replaced)
	}
}

func TestAdminTokensImportAsyncAddAndReplace(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{accounts: map[string]adminAssetsAccount{"existing": {Token: "existing", Status: "active"}}}
	refresh := &fakeAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	adminTokensAsyncRunner = func(run func()) { run() }

	rec := adminRequest(http.MethodPost, "/admin/api/tokens/import-async", `{"pool":"basic","mode":"add","tokens":["existing","new-import"],"tags":["a,b"]}`, "Bearer gork")
	rec.Result().Header.Set("Content-Type", "application/json")
	body := decodeAdminBody(t, rec)
	task := runtimepkg.GetTask(body["task_id"].(string))
	if int(body["total"].(float64)) != 2 || task == nil || task.FinalEvent()["type"] != "done" {
		t.Fatalf("import body=%#v task=%#v", body, task)
	}
	if repo.upserts[len(repo.upserts)-1][0].Token != "new-import" {
		t.Fatalf("upserts = %#v", repo.upserts)
	}

	replaceJSON := `{"mode":"replace","tokens_text":"{\"basic\":[\"r1\"],\"super\":[{\"token\":\"r2\",\"tags\":[\"x\"]}]}"}`
	rec = adminRequest(http.MethodPost, "/admin/api/tokens/import-async", replaceJSON, "Bearer gork")
	body = decodeAdminBody(t, rec)
	if int(body["total"].(float64)) != 2 || len(repo.replaced) < 2 {
		t.Fatalf("replace import body=%#v replaced=%#v", body, repo.replaced)
	}
}

func TestAdminTokensImportSpecParsesAutoNSFW(t *testing.T) {
	resetAdminRouterDepsForTest(t)

	req := httptest.NewRequest(http.MethodPost, "/admin/api/tokens/import-async?auto_nsfw=true", strings.NewReader(`{"pool":"basic","tokens":["tok"]}`))
	req.Header.Set("Content-Type", "application/json")
	spec, err := adminTokensImportSpecFromRequest(req)
	if err != nil || !spec.AutoNSFW || spec.Pool != "basic" {
		t.Fatalf("json spec=%#v err=%v", spec, err)
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/api/tokens/import-async", strings.NewReader("pool=super&tokens_text=tok&auto_nsfw=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	spec, err = adminTokensImportSpecFromRequest(req)
	if err != nil || !spec.AutoNSFW || spec.Pool != "super" || spec.Text != "tok" {
		t.Fatalf("form spec=%#v err=%v", spec, err)
	}
}

func TestAdminTokensImportAsyncAutoNSFWUsesManageableImportedTokens(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{
		accounts:       map[string]adminAssetsAccount{},
		upsertStatuses: map[string]string{"auto-disabled": "disabled"},
	}
	refresh := &fakeAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	adminTokensAsyncRunner = func(run func()) { run() }
	var sequence []string
	adminBatchNSFWSequence = func(_ context.Context, token string) error {
		sequence = append(sequence, token)
		return nil
	}

	rec := adminRequest(http.MethodPost, "/admin/api/tokens/import-async", `{"pool":"basic","mode":"add","tokens":["auto-active","auto-disabled"],"auto_nsfw":true}`, "Bearer gork")
	body := decodeAdminBody(t, rec)
	task := runtimepkg.GetTask(body["task_id"].(string))
	if rec.Code != http.StatusOK || task == nil || task.FinalEvent()["type"] != "done" {
		t.Fatalf("status/body/task=%d/%#v/%#v", rec.Code, body, task)
	}
	if len(refresh.imported) != 1 || len(refresh.imported[0]) != 2 {
		t.Fatalf("refresh imported = %#v", refresh.imported)
	}
	if len(sequence) != 1 || sequence[0] != "auto-active" {
		t.Fatalf("nsfw sequence = %#v", sequence)
	}
	if len(repo.patches) != 1 || len(repo.patches[0]) != 1 || repo.patches[0][0].Token != "auto-active" || repo.patches[0][0].AddTags[0] != "nsfw" {
		t.Fatalf("patches = %#v", repo.patches)
	}
}

func TestAdminTokensImportAsyncSurvivesRequestContextCancellation(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &cancelAwareAdminTokensRepo{fakeAdminTokensRepo: fakeAdminTokensRepo{accounts: map[string]adminAssetsAccount{}}}
	refresh := &cancelAwareAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	var runAsync func()
	adminTokensAsyncRunner = func(run func()) { runAsync = run }

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/admin/api/tokens/import-async", strings.NewReader(`{"pool":"basic","mode":"add","tokens":["new-import"]}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer gork")
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	body := decodeAdminBody(t, rec)
	if rec.Code != http.StatusOK || runAsync == nil {
		t.Fatalf("import start status/body=%d/%#v runAsync nil=%v", rec.Code, body, runAsync == nil)
	}

	cancel()
	runAsync()
	task := runtimepkg.GetTask(body["task_id"].(string))
	if task == nil {
		t.Fatalf("task missing for body=%#v", body)
	}
	final := task.FinalEvent()
	if final["type"] != "done" {
		t.Fatalf("final event = %#v", final)
	}
	if len(repo.upserts) != 1 || repo.upserts[0][0].Token != "new-import" {
		t.Fatalf("upserts = %#v", repo.upserts)
	}
	if len(refresh.imported) != 1 || refresh.imported[0][0] != "new-import" {
		t.Fatalf("refresh imported = %#v", refresh.imported)
	}
}

func TestAdminTokensRouteGoldenStatusHeadersAndShapes(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{
		listResults: []adminAssetsListResult{{
			Items: []adminAssetsAccount{{Token: "tok-list", Pool: "basic", Status: "active"}},
			Total: 1, Page: 1, PageSize: 2000, TotalPages: 1, Revision: 9,
		}},
		accounts: map[string]adminAssetsAccount{
			"existing": {Token: "existing", Status: "active"},
			"one":      {Token: "one", Status: "active"},
			"two":      {Token: "two", Status: "active"},
			"old":      {Token: "old", Pool: "basic", Status: "active"},
		},
	}
	refresh := &fakeAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	adminTokensAsyncRunner = func(run func()) { run() }

	for _, tt := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
		json   map[string]any
	}{
		{name: "list", method: http.MethodGet, path: "/admin/api/tokens", status: http.StatusOK, json: map[string]any{"pagination.total": float64(1), "revision": float64(9)}},
		{name: "save", method: http.MethodPost, path: "/admin/api/tokens", body: `{"basic":["tok-save"]}`, status: http.StatusOK, json: map[string]any{"status": "success", "count": float64(1)}},
		{name: "import async", method: http.MethodPost, path: "/admin/api/tokens/import-async", body: `{"pool":"basic","mode":"add","tokens":["new-import"]}`, status: http.StatusOK, json: map[string]any{"status": "success", "total": float64(1)}},
		{name: "add", method: http.MethodPost, path: "/admin/api/tokens/add", body: `{"tokens":["new-add"],"pool":"basic"}`, status: http.StatusOK, json: map[string]any{"status": "success", "count": float64(1), "skipped": float64(0)}},
		{name: "delete", method: http.MethodDelete, path: "/admin/api/tokens", body: `["new-add"]`, status: http.StatusOK, json: map[string]any{"deleted": float64(1)}},
		{name: "edit", method: http.MethodPut, path: "/admin/api/tokens/edit", body: `{"old_token":"old","token":"new-edit","pool":"super"}`, status: http.StatusOK, json: map[string]any{"status": "success", "token": "new-edit", "pool": "super"}},
		{name: "toggle", method: http.MethodPost, path: "/admin/api/tokens/disabled", body: `{"token":"one","disabled":true}`, status: http.StatusOK, json: map[string]any{"status": "success", "token": "one", "disabled": true}},
		{name: "toggle batch", method: http.MethodPost, path: "/admin/api/tokens/disabled/batch", body: `{"tokens":["one","two"],"disabled":false}`, status: http.StatusOK, json: map[string]any{"status": "success", "disabled": false, "summary.total": float64(2)}},
		{name: "pool", method: http.MethodPut, path: "/admin/api/tokens/pool", body: `{"pool":"heavy","tokens":["one","two"],"tags":["pool-tag"]}`, status: http.StatusOK, json: map[string]any{"pool": "heavy", "count": float64(2)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := adminRequest(tt.method, tt.path, tt.body, "Bearer gork")
			assertAdminTokensGoldenJSON(t, rec, tt.status, tt.json)
		})
	}

	rec := adminRequest(http.MethodPatch, "/admin/api/tokens", `{}`, "Bearer gork")
	assertAdminTokensGoldenJSON(t, rec, http.StatusMethodNotAllowed, map[string]any{"error.message": "Method not allowed"})
}

type fakeAdminTokensRepo struct {
	listResults    []adminAssetsListResult
	queries        []adminAssetsListQuery
	facetSnapshot  adminTokensFacetSnapshot
	facetCalls     int
	accounts       map[string]adminAssetsAccount
	upsertStatuses map[string]string
	upserts        [][]adminTokensUpsert
	patches        [][]adminBatchAccountPatch
	deleted        [][]string
	replaced       []adminTokensReplacePoolCommand
}

func (r *fakeAdminTokensRepo) ListAccounts(_ context.Context, query adminAssetsListQuery) (adminAssetsListResult, error) {
	r.queries = append(r.queries, query)
	if len(r.listResults) == 0 {
		return adminAssetsListResult{Page: query.Page, PageSize: query.PageSize, TotalPages: 1}, nil
	}
	result := r.listResults[0]
	r.listResults = r.listResults[1:]
	return result, nil
}

func (r *fakeAdminTokensRepo) ListFacets(context.Context) (adminTokensFacetSnapshot, error) {
	r.facetCalls++
	return r.facetSnapshot, nil
}

func (r *fakeAdminTokensRepo) GetAccounts(_ context.Context, tokens []string) ([]adminAssetsAccount, error) {
	out := []adminAssetsAccount{}
	for _, token := range tokens {
		if account, ok := r.accounts[token]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *fakeAdminTokensRepo) UpsertAccounts(_ context.Context, upserts []adminTokensUpsert) (adminTokensPatchResult, error) {
	r.upserts = append(r.upserts, upserts)
	if r.accounts == nil {
		r.accounts = map[string]adminAssetsAccount{}
	}
	for _, upsert := range upserts {
		status := "active"
		if r.upsertStatuses != nil && r.upsertStatuses[upsert.Token] != "" {
			status = r.upsertStatuses[upsert.Token]
		}
		r.accounts[upsert.Token] = adminAssetsAccount{
			Token:  upsert.Token,
			Pool:   upsert.Pool,
			Status: status,
			Tags:   append([]string{}, upsert.Tags...),
		}
	}
	return adminTokensPatchResult{Upserted: len(upserts), Patched: len(upserts)}, nil
}

func (r *fakeAdminTokensRepo) PatchAccounts(_ context.Context, patches []adminBatchAccountPatch) (adminTokensPatchResult, error) {
	r.patches = append(r.patches, patches)
	return adminTokensPatchResult{Patched: len(patches)}, nil
}

func (r *fakeAdminTokensRepo) DeleteAccounts(_ context.Context, tokens []string) (adminTokensPatchResult, error) {
	r.deleted = append(r.deleted, tokens)
	return adminTokensPatchResult{Deleted: len(tokens)}, nil
}

func (r *fakeAdminTokensRepo) ReplacePool(_ context.Context, cmd adminTokensReplacePoolCommand) (adminTokensPatchResult, error) {
	r.replaced = append(r.replaced, cmd)
	return adminTokensPatchResult{Upserted: len(cmd.Upserts), Patched: len(cmd.Upserts)}, nil
}

func assertAdminTokensGoldenJSON(t *testing.T, rec *httptest.ResponseRecorder, status int, want map[string]any) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, status, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q", got)
	}
	body := decodeAdminBody(t, rec)
	for key, wantValue := range want {
		gotValue, ok := adminTokensGoldenJSONValue(body, key)
		if !ok {
			t.Fatalf("json missing %q in %#v", key, body)
		}
		if gotValue != wantValue {
			t.Fatalf("json[%s]=%#v want %#v body=%s", key, gotValue, wantValue, rec.Body.String())
		}
	}
}

func adminTokensGoldenJSONValue(body map[string]any, dotted string) (any, bool) {
	current := any(body)
	for _, part := range strings.Split(dotted, ".") {
		item, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = item[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

type fakeAdminTokensRefresh struct {
	imported [][]string
}

func (f *fakeAdminTokensRefresh) RefreshOnImport(_ context.Context, tokens []string) (adminTokensRefreshResult, error) {
	f.imported = append(f.imported, append([]string{}, tokens...))
	return adminTokensRefreshResult{Refreshed: len(tokens)}, nil
}

type cancelAwareAdminTokensRepo struct {
	fakeAdminTokensRepo
}

func (r *cancelAwareAdminTokensRepo) GetAccounts(ctx context.Context, tokens []string) ([]adminAssetsAccount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.fakeAdminTokensRepo.GetAccounts(ctx, tokens)
}

func (r *cancelAwareAdminTokensRepo) UpsertAccounts(ctx context.Context, upserts []adminTokensUpsert) (adminTokensPatchResult, error) {
	if err := ctx.Err(); err != nil {
		return adminTokensPatchResult{}, err
	}
	return r.fakeAdminTokensRepo.UpsertAccounts(ctx, upserts)
}

type cancelAwareAdminTokensRefresh struct {
	fakeAdminTokensRefresh
}

func (f *cancelAwareAdminTokensRefresh) RefreshOnImport(ctx context.Context, tokens []string) (adminTokensRefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return adminTokensRefreshResult{}, err
	}
	return f.fakeAdminTokensRefresh.RefreshOnImport(ctx, tokens)
}
