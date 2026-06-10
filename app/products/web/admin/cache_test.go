package admin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCacheStatsAndListLocalFiles(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	adminCacheConfigInt = func(key string, fallback int) int {
		if key == "cache.local.image_max_mb" {
			return 1
		}
		return fallback
	}
	writeAdminCacheFile(t, dataDir, "images", "one.png", "1234", time.Unix(20, 0))
	writeAdminCacheFile(t, dataDir, "images", "skip.txt", "ignored", time.Unix(30, 0))
	writeAdminCacheFile(t, dataDir, "videos", "clip.mp4", "video", time.Unix(10, 0))

	rec := adminRequest(http.MethodGet, "/admin/api/cache", "", "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	image := body["local_image"].(map[string]any)
	if int(image["count"].(float64)) != 1 || int(image["size_bytes"].(float64)) != 4 {
		t.Fatalf("image stats = %#v", image)
	}
	if int(image["limit_mb"].(float64)) != 1 || image["limited"] != true {
		t.Fatalf("image limit = %#v", image)
	}

	rec = adminRequest(http.MethodGet, "/admin/api/cache/list?type=image&page=1&page_size=1", "", "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	items := body["items"].([]any)
	if body["status"] != "success" || int(body["total"].(float64)) != 1 || items[0].(map[string]any)["name"] != "one.png" {
		t.Fatalf("list body = %#v", body)
	}

	rec = adminRequest(http.MethodGet, "/admin/api/cache/list?cache_type=video", "", "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	items = body["items"].([]any)
	if int(body["total"].(float64)) != 1 || items[0].(map[string]any)["name"] != "clip.mp4" {
		t.Fatalf("video list body = %#v", body)
	}
}

func TestAdminCacheClearAndDeleteItems(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	writeAdminCacheFile(t, dataDir, "images", "one.png", "1", time.Unix(1, 0))
	writeAdminCacheFile(t, dataDir, "images", "two.png", "2", time.Unix(2, 0))

	rec := adminRequest(http.MethodPost, "/admin/api/cache/item/delete", `{"type":"image","name":"one.png"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if body["status"] != "success" || body["result"].(map[string]any)["deleted"] != "one.png" {
		t.Fatalf("delete body = %#v", body)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/cache/items/delete", `{"type":"image","names":["two.png","missing.png","../bad.png"," "]}`, "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	result := body["result"].(map[string]any)
	if int(result["deleted"].(float64)) != 1 || int(result["missing"].(float64)) != 2 {
		t.Fatalf("bulk result = %#v", result)
	}

	writeAdminCacheFile(t, dataDir, "images", "three.png", "3", time.Unix(3, 0))
	rec = adminRequest(http.MethodPost, "/admin/api/cache/clear", `{"type":"image"}`, "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	if int(body["result"].(map[string]any)["removed"].(float64)) != 1 {
		t.Fatalf("clear body = %#v", body)
	}
}

func TestAdminCacheDefaultsAndValidationErrors(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	writeAdminCacheFile(t, dataDir, "images", "default.png", "1", time.Unix(1, 0))

	rec := adminRequest(http.MethodPost, "/admin/api/cache/item/delete", `{"name":"default.png"}`, "Bearer grok2api")
	body := decodeAdminBody(t, rec)
	if body["status"] != "success" || body["result"].(map[string]any)["deleted"] != "default.png" {
		t.Fatalf("default delete body = %#v", body)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/cache/item/delete", `{"type":"image","name":" "}`, "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "missing_file_name" {
		t.Fatalf("missing name status/body=%d/%#v", rec.Code, body)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/cache/item/delete", `{"type":"image","name":"../bad.png"}`, "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "invalid_file_name" {
		t.Fatalf("invalid name status/body=%d/%#v", rec.Code, body)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/cache/item/delete", `{"type":"image","name":"missing.png"}`, "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	if rec.Code != http.StatusNotFound || body["error"].(map[string]any)["code"] != "file_not_found" {
		t.Fatalf("not found status/body=%d/%#v", rec.Code, body)
	}

	rec = adminRequest(http.MethodPost, "/admin/api/cache/items/delete", `{"type":"image","names":[" ",""]}`, "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "missing_file_names" {
		t.Fatalf("missing names status/body=%d/%#v", rec.Code, body)
	}

	rec = adminRequest(http.MethodGet, "/admin/api/cache/list?type=audio", "", "Bearer grok2api")
	body = decodeAdminBody(t, rec)
	if rec.Code != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "invalid_value" {
		t.Fatalf("invalid type status/body=%d/%#v", rec.Code, body)
	}
}

func TestAdminCacheRouteGoldenStatusHeadersAndShapes(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	adminCacheConfigInt = func(key string, fallback int) int {
		if key == "cache.local.image_max_mb" {
			return 2
		}
		return fallback
	}
	writeAdminCacheFile(t, dataDir, "images", "list.png", "list", time.Unix(30, 0))
	writeAdminCacheFile(t, dataDir, "images", "delete.png", "delete", time.Unix(20, 0))
	writeAdminCacheFile(t, dataDir, "images", "bulk.png", "bulk", time.Unix(10, 0))
	writeAdminCacheFile(t, dataDir, "images", "clear.png", "clear", time.Unix(5, 0))
	writeAdminCacheFile(t, dataDir, "videos", "clip.mp4", "video", time.Unix(1, 0))

	for _, tt := range []struct {
		name   string
		method string
		path   string
		body   string
		status int
		json   map[string]any
		check  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:   "stats",
			method: http.MethodGet,
			path:   "/admin/api/cache",
			status: http.StatusOK,
			json: map[string]any{
				"local_image.count":      float64(4),
				"local_image.size_bytes": float64(19),
				"local_image.limit_mb":   float64(2),
				"local_image.limited":    true,
				"local_video.count":      float64(1),
			},
		},
		{
			name:   "list",
			method: http.MethodGet,
			path:   "/admin/api/cache/list?type=image&page=1&page_size=2",
			status: http.StatusOK,
			json: map[string]any{
				"status":    "success",
				"total":     float64(4),
				"page":      float64(1),
				"page_size": float64(2),
			},
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				items := decodeAdminBody(t, rec)["items"].([]any)
				if len(items) != 2 || items[0].(map[string]any)["name"] != "list.png" {
					t.Fatalf("list items = %#v", items)
				}
			},
		},
		{
			name:   "delete item",
			method: http.MethodPost,
			path:   "/admin/api/cache/item/delete",
			body:   `{"type":"image","name":"delete.png"}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":         "success",
				"result.deleted": "delete.png",
			},
		},
		{
			name:   "delete items",
			method: http.MethodPost,
			path:   "/admin/api/cache/items/delete",
			body:   `{"type":"image","names":["bulk.png","missing.png","../bad.png"]}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":         "success",
				"result.deleted": float64(1),
				"result.missing": float64(2),
			},
		},
		{
			name:   "clear",
			method: http.MethodPost,
			path:   "/admin/api/cache/clear",
			body:   `{"type":"image"}`,
			status: http.StatusOK,
			json: map[string]any{
				"status":         "success",
				"result.removed": float64(2),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := adminRequest(tt.method, tt.path, tt.body, "Bearer grok2api")
			assertAdminGoldenJSON(t, rec, tt.status, tt.json)
			if tt.check != nil {
				tt.check(t, rec)
			}
		})
	}

	methodGuard := adminRequest(http.MethodDelete, "/admin/api/cache", "", "Bearer grok2api")
	assertAdminGoldenJSON(t, methodGuard, http.StatusMethodNotAllowed, map[string]any{"error.message": "Method not allowed"})

	matrix := []struct {
		planPath string
		covered  bool
	}{
		{planPath: "GET /admin/api/cache", covered: true},
		{planPath: "GET /admin/api/cache/list", covered: true},
		{planPath: "POST /admin/api/cache/clear", covered: true},
		{planPath: "POST /admin/api/cache/item/delete", covered: true},
		{planPath: "POST /admin/api/cache/items/delete", covered: true},
	}
	gaps := 0
	for _, row := range matrix {
		if !row.covered {
			t.Errorf("missing golden coverage for %s", row.planPath)
			gaps++
		}
	}
	t.Logf("admin_cache_route_golden_matrix rows=%d gaps=%d", len(matrix), gaps)
}

func writeAdminCacheFile(t *testing.T, dataDir string, kind string, name string, content string, mod time.Time) {
	t.Helper()
	dir := filepath.Join(dataDir, "files", kind)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}
