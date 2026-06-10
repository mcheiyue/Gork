package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const versionToken = "{{APP_VERSION}}"

var osReadFile = os.ReadFile

func ServeStaticHTML(w http.ResponseWriter, filePath string) {
	raw, err := osReadFile(filePath)
	if err != nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}
	body := strings.ReplaceAll(string(raw), versionToken, webRouterProjectVersion())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

func serveWebRouterHTML(w http.ResponseWriter, _ *http.Request, relPath string) {
	ServeStaticHTML(w, filepath.Join(webRouterStaticsRoot(), filepath.FromSlash(relPath)))
}
