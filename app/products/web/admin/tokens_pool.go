package admin

import (
	"context"
	"encoding/json"
	"net/http"
)

func handleAdminTokensPool(w http.ResponseWriter, r *http.Request) {
	repo, refresh, ok := adminTokensDeps(w)
	if !ok {
		return
	}
	var req adminTokensReplacePoolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, adminValidation("Invalid JSON body", "body"))
		return
	}
	tokens := adminTokenDedupe(req.Tokens)
	upserts := make([]adminTokensUpsert, 0, len(tokens))
	for _, token := range tokens {
		upserts = append(upserts, adminTokensUpsert{Token: token, Pool: req.Pool, Tags: req.Tags})
	}
	if _, err := repo.ReplacePool(r.Context(), adminTokensReplacePoolCommand{Pool: req.Pool, Upserts: upserts}); err != nil {
		writeAdminError(w, err)
		return
	}
	adminTokensRunRefresh(refresh, tokens)
	writeAdminJSON(w, http.StatusOK, map[string]any{"pool": req.Pool, "count": len(tokens)})
}

func adminTokensDeps(w http.ResponseWriter) (adminTokensRepository, adminTokensRefreshService, bool) {
	repo, err := adminTokensRepo()
	if err != nil {
		writeAdminError(w, err)
		return nil, nil, false
	}
	refresh, err := adminTokensRefreshSvc()
	if err != nil {
		writeAdminError(w, err)
		return nil, nil, false
	}
	return repo, refresh, true
}

func adminTokensRunRefresh(refresh adminTokensRefreshService, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	adminTokensAsyncRunner(func() {
		_, _ = refresh.RefreshOnImport(context.Background(), tokens)
	})
}
