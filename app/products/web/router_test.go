package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/auth"
)

func TestWebRouterRedirectsAndMeta(t *testing.T) {
	resetWebRouterDepsForTest(t)
	webRouterProjectVersion = func() string { return "1.2.3" }
	var forced bool
	webRouterLatestRelease = func(_ context.Context, force bool) platform.UpdateInfo {
		forced = force
		return platform.UpdateInfo{CurrentVersion: "1.2.3", Status: "ok"}
	}

	rec := getWeb("/", "")
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/admin" {
		t.Fatalf("root status/location=%d/%q", rec.Code, rec.Header().Get("Location"))
	}
	rec = getWeb("/admin", "")
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/admin/login" {
		t.Fatalf("admin status/location=%d/%q", rec.Code, rec.Header().Get("Location"))
	}
	rec = getWeb("/meta", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"1.2.3"`) {
		t.Fatalf("meta status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/meta/update?force=true", "")
	if rec.Code != http.StatusOK || !forced || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("update status/body/forced=%d/%s/%v", rec.Code, rec.Body.String(), forced)
	}
}

func TestWebRouterServesPagesAndGuardsWebUILogin(t *testing.T) {
	resetWebRouterDepsForTest(t)
	webRouterServeHTML = func(w http.ResponseWriter, _ *http.Request, path string) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("page:" + path))
	}

	rec := getWeb("/admin/login", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:admin/login.html" {
		t.Fatalf("admin login status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/admin/account", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:admin/account.html" {
		t.Fatalf("admin account status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/admin/config", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:admin/config.html" {
		t.Fatalf("admin config status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/admin/cache", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:admin/cache.html" {
		t.Fatalf("admin cache status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/webui", "")
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/webui/login" {
		t.Fatalf("webui root status/location=%d/%q", rec.Code, rec.Header().Get("Location"))
	}
	rec = getWeb("/webui/login", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled webui login status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/webui/chat", "/webui/chatkit", "/webui/masonry"} {
		rec = getWeb(path, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("disabled %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	webRouterAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIEnabled: true} }
	rec = getWeb("/webui/login", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:webui/login.html" {
		t.Fatalf("webui login status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/webui/chat", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:webui/chat.html" {
		t.Fatalf("webui chat status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/webui/chatkit", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:webui/chatkit.html" {
		t.Fatalf("webui chatkit status/body=%d/%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/webui/masonry", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "page:webui/masonry.html" {
		t.Fatalf("webui masonry status/body=%d/%s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodPost, "/webui/chat", nil)
	rec = httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("webui chat post status/allow=%d/%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestWebRouterRouteGoldenStatusHeadersAndShapes(t *testing.T) {
	resetWebRouterDepsForTest(t)
	webRouterProjectVersion = func() string { return "2.0.4" }
	webRouterLatestRelease = func(_ context.Context, force bool) platform.UpdateInfo {
		status := "current"
		if force {
			status = "forced"
		}
		return platform.UpdateInfo{CurrentVersion: "2.0.4", LatestVersion: "2.0.5", Status: status}
	}
	webRouterServeHTML = func(w http.ResponseWriter, _ *http.Request, path string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("page:" + path))
	}

	webRouterAuthSettings = func() auth.AuthSettings {
		return auth.AuthSettings{WebUIEnabled: true, WebUIKey: "web"}
	}
	for _, tt := range []struct {
		name        string
		path        string
		auth        string
		status      int
		location    string
		contentType string
		body        string
		json        map[string]any
	}{
		{name: "root redirect", path: "/", status: http.StatusTemporaryRedirect, location: "/admin"},
		{name: "admin redirect", path: "/admin", status: http.StatusTemporaryRedirect, location: "/admin/login"},
		{name: "webui redirect", path: "/webui", status: http.StatusTemporaryRedirect, location: "/webui/login"},
		{name: "admin login page", path: "/admin/login", status: http.StatusOK, contentType: "text/html", body: "page:admin/login.html"},
		{name: "admin account page", path: "/admin/account", status: http.StatusOK, contentType: "text/html", body: "page:admin/account.html"},
		{name: "admin config page", path: "/admin/config", status: http.StatusOK, contentType: "text/html", body: "page:admin/config.html"},
		{name: "admin cache page", path: "/admin/cache", status: http.StatusOK, contentType: "text/html", body: "page:admin/cache.html"},
		{name: "webui login page", path: "/webui/login", status: http.StatusOK, contentType: "text/html", body: "page:webui/login.html"},
		{name: "webui chat page", path: "/webui/chat", status: http.StatusOK, contentType: "text/html", body: "page:webui/chat.html"},
		{name: "webui chatkit page", path: "/webui/chatkit", status: http.StatusOK, contentType: "text/html", body: "page:webui/chatkit.html"},
		{name: "webui masonry page", path: "/webui/masonry", status: http.StatusOK, contentType: "text/html", body: "page:webui/masonry.html"},
		{name: "webui verify", path: "/webui/api/verify", auth: "Bearer web", status: http.StatusOK, contentType: "application/json", json: map[string]any{"status": "ok"}},
		{name: "meta", path: "/meta", status: http.StatusOK, contentType: "application/json", json: map[string]any{"version": "2.0.4"}},
		{name: "meta update", path: "/meta/update", status: http.StatusOK, contentType: "application/json", json: map[string]any{"current_version": "2.0.4", "latest_version": "2.0.5", "status": "current"}},
		{name: "meta update", path: "/meta/update?force=true", status: http.StatusOK, contentType: "application/json", json: map[string]any{"current_version": "2.0.4", "latest_version": "2.0.5", "status": "forced"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := getWeb(tt.path, tt.auth)
			if rec.Code != tt.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tt.location != "" && rec.Header().Get("Location") != tt.location {
				t.Fatalf("location=%q want %q", rec.Header().Get("Location"), tt.location)
			}
			if tt.contentType != "" && !strings.Contains(rec.Header().Get("Content-Type"), tt.contentType) {
				t.Fatalf("content-type=%q want %q", rec.Header().Get("Content-Type"), tt.contentType)
			}
			if tt.body != "" && rec.Body.String() != tt.body {
				t.Fatalf("body=%q want %q", rec.Body.String(), tt.body)
			}
			if tt.json != nil {
				assertWebJSONShape(t, rec, tt.json)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/meta", nil)
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("method guard status/allow=%d/%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestWebRouterVerifyRequiresWebUIKey(t *testing.T) {
	resetWebRouterDepsForTest(t)
	webRouterAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }

	rec := getWeb("/webui/api/verify", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = getWeb("/webui/api/verify", "Bearer web")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("valid key status/body=%d/%s", rec.Code, rec.Body.String())
	}
}

func TestWebRouterMountsWebUIAPI(t *testing.T) {
	resetWebRouterDepsForTest(t)
	rec := getWeb("/webui/api/models", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("webui api status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebRouterMountsAdminAPI(t *testing.T) {
	resetWebRouterDepsForTest(t)
	rec := getWeb("/admin/api/verify", "Bearer grok2api")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"success"`) {
		t.Fatalf("admin api status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func getWeb(path string, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	NewRouter().ServeHTTP(rec, req)
	return rec
}

func assertWebJSONShape(t *testing.T, rec *httptest.ResponseRecorder, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json decode body=%q err=%v", rec.Body.String(), err)
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok {
			t.Fatalf("json missing key %q in %#v", key, got)
		}
		if gotValue != wantValue {
			t.Fatalf("json[%s]=%#v want %#v body=%s", key, gotValue, wantValue, rec.Body.String())
		}
	}
}

func resetWebRouterDepsForTest(t *testing.T) {
	t.Helper()
	oldServeHTML := webRouterServeHTML
	oldAuth := webRouterAuthSettings
	oldVersion := webRouterProjectVersion
	oldLatest := webRouterLatestRelease
	webRouterServeHTML = func(w http.ResponseWriter, _ *http.Request, path string) {
		_, _ = w.Write([]byte(path))
	}
	webRouterAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{} }
	webRouterProjectVersion = func() string { return "test" }
	webRouterLatestRelease = func(context.Context, bool) platform.UpdateInfo { return platform.UpdateInfo{} }
	t.Cleanup(func() {
		webRouterServeHTML = oldServeHTML
		webRouterAuthSettings = oldAuth
		webRouterProjectVersion = oldVersion
		webRouterLatestRelease = oldLatest
	})
}
