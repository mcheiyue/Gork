package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/auth"
	"github.com/jiujiu532/grok2api/app/platform/config"
	adminproduct "github.com/jiujiu532/grok2api/app/products/web/admin"
	webuiapi "github.com/jiujiu532/grok2api/app/products/web/webui"
)

var (
	webRouterAuthSettings = func() auth.AuthSettings {
		return auth.AuthSettings{
			WebUIKey:     config.GetConfig("app.webui_key", ""),
			WebUIEnabled: config.GetConfig("app.webui_enabled", false),
		}
	}
	webRouterProjectVersion = platform.GetProjectVersion
	webRouterLatestRelease  = platform.GetLatestReleaseInfo
	webRouterStaticsRoot    = func() string { return filepath.Join("app", "statics") }
	webRouterServeHTML      = serveWebRouterHTML
)

// NewRouter returns the unified frontend HTTP surface.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/admin/api/", adminproduct.NewRouter())
	mux.Handle("/webui/api/", webuiapi.NewRouter())
	mux.HandleFunc("/", webMethod(http.MethodGet, handleWebRoot))
	mux.HandleFunc("/admin", webMethod(http.MethodGet, redirectWeb("/admin/login")))
	mux.HandleFunc("/admin/login", webMethod(http.MethodGet, serveWebPage("admin/login.html")))
	mux.HandleFunc("/admin/account", webMethod(http.MethodGet, serveWebPage("admin/account.html")))
	mux.HandleFunc("/admin/config", webMethod(http.MethodGet, serveWebPage("admin/config.html")))
	mux.HandleFunc("/admin/cache", webMethod(http.MethodGet, serveWebPage("admin/cache.html")))
	mux.HandleFunc("/webui", webMethod(http.MethodGet, redirectWeb("/webui/login")))
	mux.HandleFunc("/webui/login", webMethod(http.MethodGet, handleWebUILogin))
	mux.HandleFunc("/webui/chat", webMethod(http.MethodGet, serveWebUIPage("webui/chat.html")))
	mux.HandleFunc("/webui/chatkit", webMethod(http.MethodGet, serveWebUIPage("webui/chatkit.html")))
	mux.HandleFunc("/webui/masonry", webMethod(http.MethodGet, serveWebUIPage("webui/masonry.html")))
	mux.HandleFunc("/webui/api/verify", webMethod(http.MethodGet, handleWebUIVerify))
	mux.HandleFunc("/meta", webMethod(http.MethodGet, handleWebMeta))
	mux.HandleFunc("/meta/update", webMethod(http.MethodGet, handleWebUpdateMeta))
	return mux
}

func webMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

func handleWebRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusTemporaryRedirect)
}

func redirectWeb(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}
}

func serveWebPage(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		webRouterServeHTML(w, r, path)
	}
}

func handleWebUILogin(w http.ResponseWriter, r *http.Request) {
	if !auth.IsWebUIEnabled(webRouterAuthSettings()) {
		http.NotFound(w, r)
		return
	}
	webRouterServeHTML(w, r, "webui/login.html")
}

func serveWebUIPage(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.IsWebUIEnabled(webRouterAuthSettings()) {
			http.NotFound(w, r)
			return
		}
		webRouterServeHTML(w, r, path)
	}
}

func handleWebUIVerify(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyWebUIKey(r.Header.Get("Authorization"), webRouterAuthSettings()); err != nil {
		writeWebError(w, err)
		return
	}
	writeWebJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func handleWebMeta(w http.ResponseWriter, r *http.Request) {
	writeWebJSON(w, http.StatusOK, map[string]any{"version": webRouterProjectVersion()})
}

func handleWebUpdateMeta(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"
	writeWebJSON(w, http.StatusOK, webRouterLatestRelease(context.Background(), force))
}

func writeWebJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeWebError(w http.ResponseWriter, err error) {
	if appErr, ok := err.(*platform.AppError); ok {
		writeWebJSON(w, appErr.Status, appErr.ToDict())
		return
	}
	writeWebJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error()}})
}
