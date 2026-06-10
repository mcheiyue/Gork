package admin

import (
	"encoding/json"
	"net/http"
)

func handleAdminTokensSave(w http.ResponseWriter, r *http.Request) {
	repo, refresh, ok := adminTokensDeps(w)
	if !ok {
		return
	}
	payload := map[string][]any{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	count, tokens, err := adminTokensSavePayload(r, repo, payload)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	adminTokensRunRefresh(refresh, tokens)
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "count": count})
}

func handleAdminTokensAdd(w http.ResponseWriter, r *http.Request) {
	repo, refresh, ok := adminTokensDeps(w)
	if !ok {
		return
	}
	var req adminTokensAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	result, err := adminTokensAdd(r, repo, refresh, req)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, result)
}

func handleAdminTokensDelete(w http.ResponseWriter, r *http.Request) {
	repo, err := adminTokensRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var raw []string
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	tokens := adminTokenDedupe(raw)
	if len(tokens) == 0 {
		writeAdminError(w, adminValidation("No valid tokens provided", "tokens"))
		return
	}
	if _, err := repo.DeleteAccounts(r.Context(), tokens); err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"deleted": len(tokens)})
}

func handleAdminTokensEdit(w http.ResponseWriter, r *http.Request) {
	repo, err := adminTokensRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req adminTokensEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	result, err := adminTokensEdit(r, repo, req)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, result)
}

func handleAdminTokensToggle(w http.ResponseWriter, r *http.Request) {
	repo, err := adminTokensRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req adminTokensToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	result, err := adminTokensToggle(r, repo, req)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, result)
}

func handleAdminTokensToggleBatch(w http.ResponseWriter, r *http.Request) {
	repo, err := adminTokensRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req adminTokensToggleBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	result, err := adminTokensToggleBatch(r, repo, req)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, result)
}
