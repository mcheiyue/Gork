package admin

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestAdminAssetsListBuildsRowsAndTotals(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{pages: []adminAssetsListResult{{
		TotalPages: 1,
		Items: []adminAssetsAccount{
			{Token: "short-token", Status: "active"},
			{Token: "1234567890abcdefghijklmnopqrstuvwxyz", Status: "cooling"},
			{Token: "disabled-token", Status: "disabled"},
			{Token: "deleted-token", Status: "active", Deleted: true},
		},
	}}}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(_ context.Context, token string) (map[string]any, error) {
		switch token {
		case "short-token":
			return map[string]any{"assets": []any{
				map[string]any{
					"id":          "asset-1",
					"fileName":    "one.png",
					"filePath":    "/tmp/one.png",
					"contentType": "image/png",
					"fileSize":    float64(12),
					"createdAt":   "2026-01-01",
				},
			}}, nil
		case "1234567890abcdefghijklmnopqrstuvwxyz":
			return map[string]any{"items": []any{
				map[string]any{
					"assetId":      "asset-2",
					"name":         "two.jpg",
					"file_path":    "/tmp/two.jpg",
					"content_type": "image/jpeg",
					"size":         float64(34),
					"created_at":   "2026-01-02",
				},
			}}, nil
		default:
			t.Fatalf("unexpected token listed: %s", token)
		}
		return nil, nil
	}

	rec := adminRequest(http.MethodGet, "/admin/api/assets", "", "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	rows := body["tokens"].([]any)
	if len(rows) != 2 || int(body["total_assets"].(float64)) != 2 {
		t.Fatalf("body = %#v, want 2 manageable tokens and 2 assets", body)
	}
	first := rows[0].(map[string]any)
	if first["masked"] != "short-token" || int(first["count"].(float64)) != 1 {
		t.Fatalf("first row = %#v", first)
	}
	second := rows[1].(map[string]any)
	if second["masked"] != "12345678...stuvwxyz" {
		t.Fatalf("masked = %#v", second["masked"])
	}
	asset := second["assets"].([]any)[0].(map[string]any)
	if asset["id"] != "asset-2" || asset["file_path"] != "/tmp/two.jpg" || int(asset["size"].(float64)) != 34 {
		t.Fatalf("normalized asset = %#v", asset)
	}
}

func TestAdminAssetsListMarksRowErrors(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{pages: []adminAssetsListResult{{
		TotalPages: 1,
		Items:      []adminAssetsAccount{{Token: "bad-token", Status: "active"}},
	}}}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return nil, errors.New("blocked-user")
	}
	var marked []string
	adminMarkInvalidCredentials = func(_ context.Context, _ adminAssetsRepository, token string, err error, source string) bool {
		marked = append(marked, token+"|"+err.Error()+"|"+source)
		return false
	}

	rec := adminRequest(http.MethodGet, "/admin/api/assets", "", "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	rows := body["tokens"].([]any)
	row := rows[0].(map[string]any)
	if row["error"] != "blocked-user" || int(row["count"].(float64)) != 0 {
		t.Fatalf("row = %#v", row)
	}
	if len(marked) != 1 || marked[0] != "bad-token|blocked-user|asset list" {
		t.Fatalf("marked = %#v", marked)
	}
}

func TestAdminAssetsDeleteItem(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	var gotToken, gotAssetID string
	adminDeleteAsset = func(_ context.Context, token string, assetID string) error {
		gotToken, gotAssetID = token, assetID
		return nil
	}

	rec := adminRequest(http.MethodPost, "/admin/api/assets/delete-item", `{"token":"tok","asset_id":"asset-1"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if body["status"] != "success" || gotToken != "tok" || gotAssetID != "asset-1" {
		t.Fatalf("body=%#v gotToken=%q gotAssetID=%q", body, gotToken, gotAssetID)
	}
}

func TestAdminAssetsDeleteItemMarksUpstreamFailures(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminDeleteAsset = func(context.Context, string, string) error {
		return errors.New("delete blocked")
	}
	var marked []string
	adminMarkInvalidCredentials = func(_ context.Context, _ adminAssetsRepository, token string, err error, source string) bool {
		marked = append(marked, token+"|"+err.Error()+"|"+source)
		return false
	}

	rec := adminRequest(http.MethodPost, "/admin/api/assets/delete-item", `{"token":"tok","asset_id":"asset-1"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status/body=%d/%#v", rec.Code, body)
	}
	errBody := body["error"].(map[string]any)
	if errBody["code"] != "upstream_error" || errBody["message"] != "delete blocked" {
		t.Fatalf("error body = %#v", errBody)
	}
	if len(marked) != 1 || marked[0] != "tok|delete blocked|asset delete" {
		t.Fatalf("marked = %#v", marked)
	}
}

func TestAdminAssetsClearTokenDeletesKnownIDs(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"assets": []any{
			map[string]any{"id": "asset-1"},
			map[string]any{"assetId": "asset-2"},
			map[string]any{"name": "missing-id"},
		}}, nil
	}
	var deleted []string
	adminDeleteAsset = func(_ context.Context, token string, assetID string) error {
		deleted = append(deleted, token+"|"+assetID)
		return nil
	}

	rec := adminRequest(http.MethodPost, "/admin/api/assets/clear-token", `{"token":"tok"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if body["status"] != "success" || int(body["deleted"].(float64)) != 2 {
		t.Fatalf("body = %#v", body)
	}
	if len(deleted) != 2 || deleted[0] != "tok|asset-1" || deleted[1] != "tok|asset-2" {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestAdminAssetsClearTokenContinuesUntilInvalidCredentialMarked(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"assets": []any{
			map[string]any{"id": "ok-1"},
			map[string]any{"id": "transient"},
			map[string]any{"id": "invalid"},
			map[string]any{"id": "ok-2"},
		}}, nil
	}
	var attempts []string
	adminDeleteAsset = func(_ context.Context, token string, assetID string) error {
		attempts = append(attempts, token+"|"+assetID)
		switch assetID {
		case "transient":
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

	rec := adminRequest(http.MethodPost, "/admin/api/assets/clear-token", `{"token":"tok"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status/body=%d/%#v", rec.Code, body)
	}
	errBody := body["error"].(map[string]any)
	if errBody["message"] != "invalid credentials" {
		t.Fatalf("error body = %#v", errBody)
	}
	if len(attempts) != 4 || attempts[0] != "tok|ok-1" || attempts[3] != "tok|ok-2" {
		t.Fatalf("attempts = %#v", attempts)
	}
	if len(marked) != 2 || marked[0] != "tok|temporary failure|asset clear" || marked[1] != "tok|invalid credentials|asset clear" {
		t.Fatalf("marked = %#v", marked)
	}
}

func TestAdminAssetsRouteGoldenStatusHeadersAndShapes(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAdminAssetsRepo{pages: []adminAssetsListResult{{
		TotalPages: 1,
		Items:      []adminAssetsAccount{{Token: "tok-active", Status: "active"}},
	}}}
	adminAssetsRepoProvider = func() adminAssetsRepository { return repo }
	adminListAssets = func(context.Context, string) (map[string]any, error) {
		return map[string]any{"assets": []any{
			map[string]any{"id": "asset-1"},
			map[string]any{"assetId": "asset-2"},
		}}, nil
	}
	adminDeleteAsset = func(context.Context, string, string) error { return nil }

	for _, tt := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
		json   map[string]any
	}{
		{
			name:   "list assets",
			method: http.MethodGet,
			path:   "/admin/api/assets",
			status: http.StatusOK,
			json:   map[string]any{"total_assets": float64(2)},
		},
		{
			name:   "delete item",
			method: http.MethodPost,
			path:   "/admin/api/assets/delete-item",
			body:   `{"token":"tok-active","asset_id":"asset-1"}`,
			status: http.StatusOK,
			json:   map[string]any{"status": "success"},
		},
		{
			name:   "clear token",
			method: http.MethodPost,
			path:   "/admin/api/assets/clear-token",
			body:   `{"token":"tok-active"}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":  "success",
				"deleted": float64(2),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := adminRequest(tt.method, tt.path, tt.body, "Bearer grok2api")
			assertAdminGoldenJSON(t, rec, tt.status, tt.json)
			if tt.name == "list assets" {
				body := decodeAdminBody(t, rec)
				rows := body["tokens"].([]any)
				row := rows[0].(map[string]any)
				items := row["assets"].([]any)
				first := items[0].(map[string]any)
				second := items[1].(map[string]any)
				if row["token"] != "tok-active" || int(row["count"].(float64)) != 2 || first["id"] != "asset-1" || second["id"] != "asset-2" {
					t.Fatalf("list assets shape row=%#v", row)
				}
			}
		})
	}

	methodGuard := adminRequest(http.MethodDelete, "/admin/api/assets", "", "Bearer grok2api")
	assertAdminGoldenJSON(t, methodGuard, http.StatusMethodNotAllowed, map[string]any{"error.message": "Method not allowed"})

	matrix := []struct {
		planPath string
		covered  bool
	}{
		{planPath: "GET /admin/api/assets", covered: true},
		{planPath: "POST /admin/api/assets/delete-item", covered: true},
		{planPath: "POST /admin/api/assets/clear-token", covered: true},
	}
	gaps := 0
	for _, row := range matrix {
		if !row.covered {
			t.Errorf("missing golden coverage for %s", row.planPath)
			gaps++
		}
	}
	t.Logf("admin_assets_route_golden_matrix rows=%d gaps=%d", len(matrix), gaps)
}

type fakeAdminAssetsRepo struct {
	pages []adminAssetsListResult
}

func (r *fakeAdminAssetsRepo) ListAccounts(_ context.Context, query adminAssetsListQuery) (adminAssetsListResult, error) {
	if query.Page <= 0 || query.Page > len(r.pages) {
		return adminAssetsListResult{TotalPages: 1}, nil
	}
	return r.pages[query.Page-1], nil
}
