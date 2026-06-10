package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAppRoutesHealthStaticFaviconAndProductRouters(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	staticRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticRoot, "app.js"), []byte("console.log('ok')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticRoot, "favicon.ico"), []byte("ico"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp(AppOptions{
		StaticsRoot:     staticRoot,
		WebRouter:       textHandler("web"),
		OpenAIRouter:    textHandler("openai"),
		AnthropicRouter: textHandler("anthropic"),
	})

	assertAppResponse(t, app.Handler(), http.MethodGet, "/health", "", http.StatusOK, `"status":"ok"`)
	assertAppResponse(t, app.Handler(), http.MethodGet, "/static/app.js", "", http.StatusOK, "console.log")
	assertAppResponse(t, app.Handler(), http.MethodGet, "/favicon.ico", "", http.StatusOK, "ico")
	assertAppResponse(t, app.Handler(), http.MethodGet, "/v1/models", "", http.StatusOK, "openai")
	assertAppResponse(t, app.Handler(), http.MethodPost, "/v1/messages", "", http.StatusOK, "anthropic")
	assertAppResponse(t, app.Handler(), http.MethodGet, "/admin", "", http.StatusOK, "web")
}

func TestNewAppCORSAndErrorRecovery(t *testing.T) {
	stubAppMainRequestMiddleware(t)
	app := NewApp(AppOptions{
		WebRouter: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}),
	})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Origin", "https://example.test")
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Internal server error") {
		t.Fatalf("panic response=%d/%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("missing CORS headers: %#v", rec.Header())
	}
}

func TestNewAppReloadsConfigAndReconcilesRefreshRuntimePerRequest(t *testing.T) {
	resetAppMainDeps(t)
	loadCalls := 0
	reconcileCalls := 0
	appMainLoadRequestConfig = func(context.Context) error {
		loadCalls++
		return nil
	}
	appMainReconcileRefreshRuntime = func() string {
		reconcileCalls++
		return "random"
	}
	app := NewApp(AppOptions{WebRouter: textHandler("web")})

	assertAppResponse(t, app.Handler(), http.MethodGet, "/admin", "", http.StatusOK, "web")
	if loadCalls != 1 || reconcileCalls != 1 {
		t.Fatalf("middleware calls load=%d reconcile=%d", loadCalls, reconcileCalls)
	}
}

func TestDefaultLifecycleWiresAdminAccountRuntime(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("PORT", "0")

	app := NewApp(AppOptions{})
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start error: %v", err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown error: %v", err)
		}
	})

	status := appJSONRequest(t, app.Handler(), http.MethodGet, "/admin/api/status", "", "Bearer grok2api")
	if code := adminErrorCode(status); code == "directory_not_initialised" {
		t.Fatalf("admin status returned directory_not_initialised: %#v", status)
	}
	if status["status"] != "ok" {
		t.Fatalf("admin status response = %#v", status)
	}

	tokens := appJSONRequest(t, app.Handler(), http.MethodGet, "/admin/api/tokens?page=1&page_size=50&sort_by=updated_at&sort_desc=true", "", "Bearer grok2api")
	if code := adminErrorCode(tokens); code == "account_repository_not_initialised" {
		t.Fatalf("admin tokens returned account_repository_not_initialised: %#v", tokens)
	}
	if _, ok := tokens["tokens"]; !ok {
		t.Fatalf("admin tokens response missing tokens: %#v", tokens)
	}
}

func TestAppLifecycleRunsHooksInOrderAndStopsOnStartupError(t *testing.T) {
	events := []string{}
	app := NewApp(AppOptions{
		StartupHooks: []Hook{
			func(context.Context) error { events = append(events, "config"); return nil },
			func(context.Context) error { events = append(events, "repository"); return nil },
		},
		ShutdownHooks: []Hook{
			func(context.Context) error { events = append(events, "refresh-stop"); return nil },
			func(context.Context) error { events = append(events, "runtime-close"); return nil },
		},
	})
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	if got := strings.Join(events, ","); got != "config,repository,refresh-stop,runtime-close" {
		t.Fatalf("events=%s", got)
	}

	fail := errors.New("load failed")
	app = NewApp(AppOptions{StartupHooks: []Hook{
		func(context.Context) error { events = append(events, "first"); return fail },
		func(context.Context) error { events = append(events, "second"); return nil },
	}})
	if err := app.Start(context.Background()); !errors.Is(err, fail) {
		t.Fatalf("startup error=%v", err)
	}
	if strings.Contains(strings.Join(events, ","), "second") {
		t.Fatalf("startup continued after error: %v", events)
	}
}

func TestNewAppUsesDefaultStartupHooksWhenNoneProvided(t *testing.T) {
	resetAppMainDeps(t)
	events := []string{}
	appMainEnsureConfig = func(context.Context) error {
		events = append(events, "config")
		return nil
	}
	appMainSetupLogging = func() error {
		events = append(events, "logging")
		return nil
	}
	appMainStartRuntimeStore = recordLifecycleStep("runtime-store", "", &events)
	appMainConfigureTaskSnapshotStore = recordLifecycleStep("task-snapshot-store", "", &events)
	appMainInitializeRepository = recordLifecycleStep("repository", "", &events)
	appMainRunStartupMigrations = recordLifecycleStep("startup-migrations", "", &events)
	appMainReconcileLocalMediaCache = recordLifecycleStep("media-cache", "", &events)
	appMainStartAccountDirectory = recordLifecycleStep("account-directory", "", &events)
	appMainStartRefreshRuntime = recordLifecycleStep("refresh-runtime", "", &events)
	appMainStartProxyScheduler = recordLifecycleStep("proxy-scheduler", "", &events)

	app := NewApp(AppOptions{})
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if got := strings.Join(events, ","); got != "config,logging,runtime-store,task-snapshot-store,repository,startup-migrations,media-cache,account-directory,refresh-runtime,proxy-scheduler" {
		t.Fatalf("default startup events=%s", got)
	}
}

func TestDefaultLifecycleMirrorsPythonLifespanStartupAndShutdown(t *testing.T) {
	resetAppMainDeps(t)
	events := []string{}
	appMainEnsureConfig = func(context.Context) error {
		events = append(events, "config")
		return nil
	}
	appMainSetupLogging = func() error {
		events = append(events, "logging")
		return nil
	}
	appMainStartRuntimeStore = recordLifecycleStep("runtime-store", "runtime-store-close", &events)
	appMainConfigureTaskSnapshotStore = recordLifecycleStep("task-snapshot-store", "task-snapshot-store-clear", &events)
	appMainInitializeRepository = recordLifecycleStep("repository", "repository-close", &events)
	appMainRunStartupMigrations = recordLifecycleStep("startup-migrations", "", &events)
	appMainReconcileLocalMediaCache = recordLifecycleStep("media-cache", "", &events)
	appMainStartAccountDirectory = recordLifecycleStep("account-directory", "account-directory-stop", &events)
	appMainStartRefreshRuntime = recordLifecycleStep("refresh-runtime", "refresh-runtime-stop", &events)
	appMainStartProxyScheduler = recordLifecycleStep("proxy-scheduler", "proxy-scheduler-stop", &events)

	app := NewApp(AppOptions{})
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}
	want := strings.Join([]string{
		"config",
		"logging",
		"runtime-store",
		"task-snapshot-store",
		"repository",
		"startup-migrations",
		"media-cache",
		"account-directory",
		"refresh-runtime",
		"proxy-scheduler",
		"proxy-scheduler-stop",
		"refresh-runtime-stop",
		"account-directory-stop",
		"repository-close",
		"task-snapshot-store-clear",
		"runtime-store-close",
	}, ",")
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("default lifecycle events=%s, want %s", got, want)
	}
}

func resetAppMainDeps(t *testing.T) {
	t.Helper()
	oldEnsureConfig := appMainEnsureConfig
	oldLoadRequestConfig := appMainLoadRequestConfig
	oldReconcileRefreshRuntime := appMainReconcileRefreshRuntime
	oldSetupLogging := appMainSetupLogging
	oldStartRuntimeStore := appMainStartRuntimeStore
	oldConfigureTaskSnapshotStore := appMainConfigureTaskSnapshotStore
	oldInitializeRepository := appMainInitializeRepository
	oldRunStartupMigrations := appMainRunStartupMigrations
	oldReconcileLocalMediaCache := appMainReconcileLocalMediaCache
	oldStartAccountDirectory := appMainStartAccountDirectory
	oldStartRefreshRuntime := appMainStartRefreshRuntime
	oldStartProxyScheduler := appMainStartProxyScheduler
	t.Cleanup(func() {
		appMainEnsureConfig = oldEnsureConfig
		appMainLoadRequestConfig = oldLoadRequestConfig
		appMainReconcileRefreshRuntime = oldReconcileRefreshRuntime
		appMainSetupLogging = oldSetupLogging
		appMainStartRuntimeStore = oldStartRuntimeStore
		appMainConfigureTaskSnapshotStore = oldConfigureTaskSnapshotStore
		appMainInitializeRepository = oldInitializeRepository
		appMainRunStartupMigrations = oldRunStartupMigrations
		appMainReconcileLocalMediaCache = oldReconcileLocalMediaCache
		appMainStartAccountDirectory = oldStartAccountDirectory
		appMainStartRefreshRuntime = oldStartRefreshRuntime
		appMainStartProxyScheduler = oldStartProxyScheduler
	})
}

func recordLifecycleStep(start, stop string, events *[]string) appMainLifecycleStep {
	return func(context.Context, *appMainLifecycleState) (Hook, error) {
		*events = append(*events, start)
		if stop == "" {
			return nil, nil
		}
		return func(context.Context) error {
			*events = append(*events, stop)
			return nil
		}, nil
	}
}

func textHandler(text string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(text))
	})
}

func stubAppMainRequestMiddleware(t *testing.T) {
	t.Helper()
	resetAppMainDeps(t)
	appMainLoadRequestConfig = func(context.Context) error { return nil }
	appMainReconcileRefreshRuntime = func() string { return "random" }
}

func assertAppResponse(t *testing.T, handler http.Handler, method, target, body string, status int, contains string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != status || !strings.Contains(rec.Body.String(), contains) {
		t.Fatalf("%s %s = %d/%s, want %d containing %q", method, target, rec.Code, rec.Body.String(), status, contains)
	}
}

func appJSONRequest(t *testing.T, handler http.Handler, method, target, body string, authorization string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("%s %s returned invalid JSON status=%d body=%s", method, target, rec.Code, rec.Body.String())
	}
	if rec.Code >= http.StatusInternalServerError {
		t.Fatalf("%s %s returned server error status=%d body=%s", method, target, rec.Code, rec.Body.String())
	}
	return payload
}

func adminErrorCode(payload map[string]any) string {
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := errorPayload["code"].(string)
	return code
}
