package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/storage"
)

func handleAdminCacheStats(w http.ResponseWriter, _ *http.Request) {
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"local_image": adminCacheStats(storage.MediaTypeImage),
		"local_video": adminCacheStats(storage.MediaTypeVideo),
	})
}

func handleAdminCacheList(w http.ResponseWriter, r *http.Request) {
	mediaType, err := adminCacheListType(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	page := adminCachePositiveQuery(r, "page", 1)
	pageSize := adminCachePositiveQuery(r, "page_size", 1000)
	payload := adminCacheListFiles(mediaType, page, pageSize)
	payload["status"] = "success"
	writeAdminJSON(w, http.StatusOK, payload)
}

func handleAdminCacheClear(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAdminCacheTypeRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	removed, err := adminCacheStoreProvider().Clear(req.Type)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "result": map[string]any{"removed": removed}})
}

func handleAdminCacheDeleteItem(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAdminCacheDeleteRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	deleted, err := adminCacheStoreProvider().Delete(req.Type, req.Name)
	if err != nil {
		writeAdminError(w, adminCacheInvalidFileName(err))
		return
	}
	if !deleted {
		writeAdminError(w, adminCacheFileNotFound())
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "result": map[string]any{"deleted": req.Name}})
}

func handleAdminCacheDeleteItems(w http.ResponseWriter, r *http.Request) {
	req, err := decodeAdminCacheDeleteItemsRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	deleted, missing := adminCacheDeleteNames(req.Type, req.Names)
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"status": "success", "result": map[string]any{"deleted": deleted, "missing": missing},
	})
}

type adminCacheTypeRequest struct {
	Type storage.MediaType `json:"type"`
}

type adminCacheDeleteRequest struct {
	Type storage.MediaType `json:"type"`
	Name string            `json:"name"`
}

type adminCacheDeleteItemsRequest struct {
	Type  storage.MediaType `json:"type"`
	Names []string          `json:"names"`
}

func decodeAdminCacheTypeRequest(r *http.Request) (adminCacheTypeRequest, error) {
	var req adminCacheTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	return req, adminCacheNormalizeType(&req.Type)
}

func decodeAdminCacheDeleteRequest(r *http.Request) (adminCacheDeleteRequest, error) {
	var req adminCacheDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	if err := adminCacheNormalizeType(&req.Type); err != nil {
		return req, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return req, platform.NewAppError("Missing file name", platform.ErrorKindValidation, "missing_file_name", 400, nil)
	}
	return req, nil
}

func decodeAdminCacheDeleteItemsRequest(r *http.Request) (adminCacheDeleteItemsRequest, error) {
	var req adminCacheDeleteItemsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	if err := adminCacheNormalizeType(&req.Type); err != nil {
		return req, err
	}
	req.Names = adminCacheCleanNames(req.Names)
	if len(req.Names) == 0 {
		return req, platform.NewAppError("Missing file names", platform.ErrorKindValidation, "missing_file_names", 400, nil)
	}
	return req, nil
}

func adminCacheListType(r *http.Request) (storage.MediaType, error) {
	raw := r.URL.Query().Get("type")
	if raw == "" {
		raw = r.URL.Query().Get("cache_type")
	}
	mediaType := storage.MediaType(raw)
	return mediaType, adminCacheNormalizeType(&mediaType)
}

func adminCachePositiveQuery(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}
