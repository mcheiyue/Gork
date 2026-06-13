package admin

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

func handleAdminTokensImportAsync(w http.ResponseWriter, r *http.Request) {
	repo, refresh, ok := adminTokensDeps(w)
	if !ok {
		return
	}
	spec, err := adminTokensImportSpecFromRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	payload, total, err := adminTokensImportPayload(&spec)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	task := runtimepkg.CreateTask(total)
	taskCtx := context.Background()
	if spec.Replace {
		adminTokensAsyncRunner(func() { adminTokensRunReplaceImport(taskCtx, repo, refresh, task, payload, spec.AutoNSFW) })
	} else {
		adminTokensAsyncRunner(func() {
			adminTokensRunAddImport(taskCtx, repo, refresh, task, spec.Pool, spec.Tokens, spec.Tags, spec.AutoNSFW)
		})
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "task_id": task.ID, "total": total})
}

type adminTokensImportSpec struct {
	Pool     string
	Mode     string
	Text     string
	Tags     []string
	Replace  bool
	Tokens   []string
	AutoNSFW bool
}

func adminTokensImportSpecFromRequest(r *http.Request) (adminTokensImportSpec, error) {
	contentType := strings.ToLower(r.Header.Get("content-type"))
	if strings.Contains(contentType, "multipart/form-data") || strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return adminTokensImportSpecFromForm(r)
	}
	return adminTokensImportSpecFromJSON(r)
}

func adminTokensImportSpecFromJSON(r *http.Request) (adminTokensImportSpec, error) {
	var req adminTokensImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return adminTokensImportSpec{}, adminValidation("Invalid JSON body", "body")
	}
	text := req.TokensText
	if text == "" && len(req.Tokens) > 0 {
		text = strings.Join(adminTokenStringSlice(req.Tokens), "\n")
	}
	autoNSFW, err := adminTokensAutoNSFWFromQuery(r, req.AutoNSFW)
	if err != nil {
		return adminTokensImportSpec{}, err
	}
	return adminTokensImportSpec{Pool: adminTokenPool(req.Pool), Mode: adminTokenPool(req.Mode), Text: text, Tags: adminTokenTags(req.Tags), AutoNSFW: autoNSFW}, nil
}

func adminTokensImportSpecFromForm(r *http.Request) (adminTokensImportSpec, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
		return adminTokensImportSpec{}, err
	}
	autoNSFW, err := adminTokensBoolParam(r.FormValue("auto_nsfw"), false)
	if err != nil {
		return adminTokensImportSpec{}, err
	}
	spec := adminTokensImportSpec{Pool: adminTokenPool(r.FormValue("pool")), Mode: adminTokenPool(r.FormValue("mode")), Text: r.FormValue("tokens_text"), Tags: adminTokenTags(r.FormValue("tags")), AutoNSFW: autoNSFW}
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		return adminTokensImportSpecFromFile(spec, file, header)
	}
	return spec, nil
}

func adminTokensAutoNSFWFromQuery(r *http.Request, fallback bool) (bool, error) {
	return adminTokensBoolParam(r.URL.Query().Get("auto_nsfw"), fallback)
}

func adminTokensBoolParam(raw string, fallback bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, adminValidation("Invalid boolean query parameter", "auto_nsfw")
	}
	return value, nil
}

func adminTokensImportSpecFromFile(spec adminTokensImportSpec, file multipart.File, header *multipart.FileHeader) (adminTokensImportSpec, error) {
	raw, err := io.ReadAll(file)
	if err != nil {
		return spec, err
	}
	spec.Text = strings.TrimPrefix(string(raw), "\ufeff")
	if strings.HasSuffix(strings.ToLower(header.Filename), ".json") {
		spec.Replace = true
	}
	return spec, nil
}

func adminTokensImportPayload(spec *adminTokensImportSpec) (map[string][]adminTokensUpsert, int, error) {
	if spec.Replace || spec.Mode == "replace" && strings.HasPrefix(strings.TrimSpace(spec.Text), "{") {
		spec.Replace = true
		payload, err := adminTokensPoolPayloadFromJSON(spec.Text)
		return payload, adminTokensPayloadTotal(payload), err
	}
	tokens := adminTokensFromText(spec.Text)
	if len(tokens) == 0 {
		return nil, 0, adminValidation("No valid tokens provided", "tokens")
	}
	spec.Tokens = tokens
	return nil, len(tokens), nil
}

func adminTokensPayloadTotal(payload map[string][]adminTokensUpsert) int {
	total := 0
	for _, upserts := range payload {
		total += len(upserts)
	}
	return total
}
