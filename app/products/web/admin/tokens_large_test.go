package admin

import (
	"net/http"
	"reflect"
	"testing"

	runtimepkg "github.com/jiujiu532/grok2api/app/platform/runtime"
)

func TestAdminTokensRepositoryFiltersTagsBeforePagination(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{
		listResults: []adminAssetsListResult{{
			Items: []adminAssetsAccount{
				{Token: "tok-001", Tags: []string{"nsfw"}},
				{Token: "tok-003", Tags: []string{"nsfw"}},
			},
			Total: 2, Page: 1, PageSize: 10, TotalPages: 1,
		}},
	}

	result, err := repo.ListAccounts(t.Context(), adminAssetsListQuery{
		Tags: []string{"nsfw"}, Page: 1, PageSize: 10, SortBy: "token",
	})
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	got := []string{}
	for _, item := range result.Items {
		got = append(got, item.Token)
	}
	if result.Total != 2 || !reflect.DeepEqual(got, []string{"tok-001", "tok-003"}) {
		t.Fatalf("filtered page total=%d tokens=%v", result.Total, got)
	}
	if len(repo.queries) != 1 || !reflect.DeepEqual(repo.queries[0].Tags, []string{"nsfw"}) {
		t.Fatalf("query did not preserve tag filter before pagination: %#v", repo.queries)
	}
}

func TestAdminTokensListLargeSetReturnsPageWithGlobalFacets(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{
		listResults: []adminAssetsListResult{{
			Items: []adminAssetsAccount{
				{Token: "tok-003", Pool: "basic", Status: "active"},
				{Token: "tok-004", Pool: "super", Status: "active"},
			},
			Total: 5, Page: 2, PageSize: 2, TotalPages: 3,
		}},
		facetSnapshot: adminTokensFacetSnapshotFromRecords([]adminAssetsAccount{
			{Token: "tok-001", Pool: "basic", Status: "active", Tags: []string{"nsfw"}},
			{Token: "tok-002", Pool: "basic", Status: "active"},
			{Token: "tok-003", Pool: "basic", Status: "active", Tags: []string{"nsfw"}},
			{Token: "tok-004", Pool: "super", Status: "active"},
			{Token: "tok-005", Pool: "heavy", Status: "active"},
		}),
	}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }

	rec := adminRequest(http.MethodGet, "/admin/api/tokens?page=2&page_size=2&sort_by=token&sort_desc=false", "", "Bearer grok2api")
	body := decodeAdminBody(t, rec)

	tokens := body["tokens"].([]any)
	gotTokens := []string{}
	for _, item := range tokens {
		gotTokens = append(gotTokens, item.(map[string]any)["token"].(string))
	}
	if !reflect.DeepEqual(gotTokens, []string{"tok-003", "tok-004"}) {
		t.Fatalf("tokens page = %v", gotTokens)
	}
	pagination := body["pagination"].(map[string]any)
	wantPagination := map[string]int{"total": 5, "page": 2, "page_size": 2, "total_pages": 3}
	for key, want := range wantPagination {
		if got := int(pagination[key].(float64)); got != want {
			t.Fatalf("pagination[%s] = %d, want %d; full=%#v", key, got, want, pagination)
		}
	}
	facets := body["facets"].(map[string]any)
	pools := facets["pools"].(map[string]any)
	status := facets["status"].(map[string]any)
	nsfw := facets["nsfw"].(map[string]any)
	if int(pools["basic"].(float64)) != 3 || int(pools["super"].(float64)) != 1 || int(pools["heavy"].(float64)) != 1 {
		t.Fatalf("pool facets = %#v", pools)
	}
	if int(status["active"].(float64)) != 5 {
		t.Fatalf("status facets = %#v", status)
	}
	if int(nsfw["enabled"].(float64)) != 2 || int(nsfw["disabled"].(float64)) != 3 {
		t.Fatalf("nsfw facets = %#v", nsfw)
	}
	if len(repo.queries) != 1 || repo.facetCalls != 1 {
		t.Fatalf("list/facet calls = %d/%d", len(repo.queries), repo.facetCalls)
	}
}

func TestAdminTokensAsyncTextImportReportsTaskProgress(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminTokensRepo{}
	refresh := &fakeAdminTokensRefresh{}
	adminTokensRepoProvider = func() adminTokensRepository { return repo }
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return refresh }
	adminTokensAsyncRunner = func(run func()) { run() }

	rec := adminRequest(http.MethodPost, "/admin/api/tokens/import-async", `{"pool":"basic","tokens_text":"tok-100\ntok-101\ntok-100\n"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)

	if body["status"] != "success" || int(body["total"].(float64)) != 2 {
		t.Fatalf("import body = %#v", body)
	}
	task := runtimepkg.GetTask(body["task_id"].(string))
	if task == nil || task.Status != "done" {
		t.Fatalf("task = %#v", task)
	}
	final := task.FinalEvent()
	result := final["result"].(map[string]any)
	summary := result["summary"].(map[string]any)
	wantSummary := map[string]int{"total": 2, "ok": 2, "fail": 0, "skipped": 0}
	for key, want := range wantSummary {
		if got := summary[key].(int); got != want {
			t.Fatalf("summary[%s] = %d, want %d; full=%#v", key, got, want, summary)
		}
	}
	if len(repo.upserts) != 1 || len(repo.upserts[0]) != 2 {
		t.Fatalf("upserts = %#v", repo.upserts)
	}
	gotTokens := []string{repo.upserts[0][0].Token, repo.upserts[0][1].Token}
	if !reflect.DeepEqual(gotTokens, []string{"tok-100", "tok-101"}) {
		t.Fatalf("upsert tokens = %v", gotTokens)
	}
	if !reflect.DeepEqual(refresh.imported, [][]string{{"tok-100", "tok-101"}}) {
		t.Fatalf("refresh calls = %#v", refresh.imported)
	}
}
