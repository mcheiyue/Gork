package openai

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/storage"
)

var routerFileIDRE = regexp.MustCompile(`^[0-9a-f\-]{16,36}$`)

func handleServeVideo(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("id")
	if !routerFileIDRE.MatchString(fileID) {
		writeRouterError(w, platform.NewValidationError("Invalid file ID", "id", ""))
		return
	}
	dir, err := storage.VideoFilesDir()
	if err != nil {
		writeRouterError(w, err)
		return
	}
	path := filepath.Join(dir, fileID+".mp4")
	if !serveRouterFile(w, path, "video/mp4") {
		writeRouterError(w, platform.NewValidationError("Video '"+fileID+"' not found", "id", ""))
	}
}

func handleServeImage(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("id")
	if !routerFileIDRE.MatchString(fileID) {
		writeRouterError(w, platform.NewValidationError("Invalid file ID", "id", ""))
		return
	}
	dir, err := storage.ImageFilesDir()
	if err != nil {
		writeRouterError(w, err)
		return
	}
	if serveRouterFile(w, filepath.Join(dir, fileID+".jpg"), "image/jpeg") {
		return
	}
	if serveRouterFile(w, filepath.Join(dir, fileID+".png"), "image/png") {
		return
	}
	writeRouterError(w, platform.NewValidationError("Image '"+fileID+"' not found", "id", ""))
}

func serveRouterFile(w http.ResponseWriter, path string, contentType string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
	return true
}
