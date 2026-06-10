package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServeStaticHTMLReplacesVersionToken(t *testing.T) {
	resetWebRouterDepsForTest(t)
	webRouterProjectVersion = func() string { return "9.9.9" }
	path := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(path, []byte("<html>{{APP_VERSION}}</html>"), 0o644); err != nil {
		t.Fatalf("write temp html: %v", err)
	}

	rec := httptest.NewRecorder()
	ServeStaticHTML(rec, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "<html>9.9.9</html>" {
		t.Fatalf("body=%q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("content-type=%q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control=%q", rec.Header().Get("Cache-Control"))
	}
}

func TestServeStaticHTMLKeepsBodyWithoutVersionToken(t *testing.T) {
	resetWebRouterDepsForTest(t)
	path := filepath.Join(t.TempDir(), "plain.html")
	if err := os.WriteFile(path, []byte("<html>plain body</html>"), 0o644); err != nil {
		t.Fatalf("write temp html: %v", err)
	}

	rec := httptest.NewRecorder()
	ServeStaticHTML(rec, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "<html>plain body</html>" {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestServeStaticHTMLMissingFile(t *testing.T) {
	resetWebRouterDepsForTest(t)
	rec := httptest.NewRecorder()
	ServeStaticHTML(rec, filepath.Join(t.TempDir(), "missing.html"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
