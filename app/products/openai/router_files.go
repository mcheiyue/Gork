package openai

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/storage"
)

var routerFileIDRE = regexp.MustCompile(`^[0-9a-f\-]{16,36}$`)

func handleServeVideo(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("id")
	if !routerFileIDRE.MatchString(fileID) {
		writeRouterError(w, platform.NewValidationError("Invalid file ID", "id", ""))
		return
	}
	if !verifyRouterFileRequest(w, r, fileID) {
		return
	}
	dir, err := storage.VideoFilesDir()
	if err != nil {
		writeRouterError(w, err)
		return
	}
	path := filepath.Join(dir, fileID+".mp4")
	if !serveRouterFile(w, r, path, "video/mp4") {
		writeRouterError(w, platform.NewValidationError("Video '"+fileID+"' not found", "id", ""))
	}
}

func handleServeImage(w http.ResponseWriter, r *http.Request) {
	fileID := r.URL.Query().Get("id")
	if !routerFileIDRE.MatchString(fileID) {
		writeRouterError(w, platform.NewValidationError("Invalid file ID", "id", ""))
		return
	}
	if !verifyRouterFileRequest(w, r, fileID) {
		return
	}
	dir, err := storage.ImageFilesDir()
	if err != nil {
		writeRouterError(w, err)
		return
	}
	if serveRouterFile(w, r, filepath.Join(dir, fileID+".jpg"), "image/jpeg") {
		return
	}
	if serveRouterFile(w, r, filepath.Join(dir, fileID+".png"), "image/png") {
		return
	}
	writeRouterError(w, platform.NewValidationError("Image '"+fileID+"' not found", "id", ""))
}

func verifyRouterFileRequest(w http.ResponseWriter, r *http.Request, fileID string) bool {
	expRaw := strings.TrimSpace(r.URL.Query().Get("exp"))
	sig := strings.TrimSpace(r.URL.Query().Get("sig"))
	if expRaw == "" && sig == "" {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Warning", `299 - "unsigned media URLs are deprecated; use exp and sig"`)
		return true
	}
	if expRaw == "" || sig == "" {
		writeRouterError(w, platform.NewValidationError("Invalid media signature", "sig", ""))
		return false
	}
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil {
		writeRouterError(w, platform.NewValidationError("Invalid media signature", "exp", ""))
		return false
	}
	if routerMediaNow().Unix() > exp {
		writeRouterError(w, platform.NewValidationError("Expired media URL", "exp", ""))
		return false
	}
	if !routerMediaSignatureValid(fileID, exp, sig) {
		writeRouterError(w, platform.NewValidationError("Invalid media signature", "sig", ""))
		return false
	}
	return true
}

func serveRouterFile(w http.ResponseWriter, r *http.Request, path string, contentType string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		return false
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, stat.ModTime().UnixNano(), stat.Size()))
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
	return true
}
