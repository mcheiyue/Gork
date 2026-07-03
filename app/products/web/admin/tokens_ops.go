package admin

import (
	"net/http"

	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/logging"
)

func adminTokensSavePayload(r *http.Request, repo adminTokensRepository, payload map[string][]any) (int, []string, error) {
	total := 0
	allTokens := []string{}
	for pool, items := range payload {
		upserts := adminTokensUpsertsFromItems(pool, items)
		if len(upserts) == 0 {
			continue
		}
		if _, err := repo.ReplacePool(r.Context(), adminTokensReplacePoolCommand{Pool: pool, Upserts: upserts}); err != nil {
			return 0, nil, err
		}
		total += len(upserts)
		for _, upsert := range upserts {
			allTokens = append(allTokens, upsert.Token)
		}
	}
	return total, allTokens, nil
}

func adminTokensAdd(r *http.Request, repo adminTokensRepository, refresh adminTokensRefreshService, req adminTokensAddRequest) (map[string]any, error) {
	pool := adminTokenPool(req.Pool)
	tokens := adminTokenDedupe(req.Tokens)
	if len(tokens) == 0 {
		return nil, adminValidation("No valid tokens provided", "tokens")
	}
	existing, err := repo.GetAccounts(r.Context(), tokens)
	if err != nil {
		return nil, err
	}
	newTokens := adminTokensNewOnly(tokens, existing)
	if len(newTokens) == 0 {
		return map[string]any{"status": "success", "count": 0, "skipped": len(tokens)}, nil
	}
	result, err := repo.UpsertAccounts(r.Context(), adminTokensUpserts(newTokens, pool, req.Tags))
	if err != nil {
		return nil, err
	}
	if pool == "auto" {
		_, _ = refresh.RefreshOnImport(r.Context(), newTokens)
	} else {
		adminTokensRunRefresh(refresh, newTokens)
	}
	response := map[string]any{"status": "success", "count": adminTokensUpserted(result, len(newTokens)), "skipped": len(tokens) - len(newTokens), "synced": pool == "auto"}
	if req.AutoNSFW {
		nsfwCount, err := adminTokensEnableAutoNSFW(r.Context(), repo, newTokens)
		if err != nil {
			return nil, err
		}
		response["nsfw"] = nsfwCount
	}
	return response, nil
}

func adminTokensEdit(r *http.Request, repo adminTokensRepository, req adminTokensEditRequest) (map[string]any, error) {
	oldToken, newToken := adminTokenSanitize(req.OldToken), adminTokenSanitize(req.Token)
	if oldToken == "" || newToken == "" {
		return nil, adminValidation("Token is required", "token")
	}
	records, err := repo.GetAccounts(r.Context(), []string{oldToken})
	if err != nil || len(records) == 0 {
		return nil, adminAccountNotFound()
	}
	if oldToken != newToken {
		if err := adminTokensEnsureTargetFree(r, repo, newToken); err != nil {
			return nil, err
		}
	}
	record := records[0]
	pool := adminTokenPool(req.Pool)
	if oldToken == newToken {
		patch := adminTokensEditPatch(newToken, pool, record)
		if _, err := repo.PatchAccounts(r.Context(), []adminBatchAccountPatch{patch}); err != nil {
			return nil, err
		}
	} else {
		if _, err := repo.UpsertAccounts(r.Context(), []adminTokensUpsert{{Token: newToken, Pool: pool, Tags: record.Tags, Ext: record.Ext}}); err != nil {
			return nil, err
		}
		if err := adminTokensCopyStateAndDeleteOld(r, repo, oldToken, newToken, pool, record); err != nil {
			return nil, err
		}
	}
	if oldToken == newToken {
		logging.Logger.Info("admin token updated", "token", adminTokenMask(newToken), "pool", pool)
	} else {
		logging.Logger.Info("admin token replaced", "previous_token", adminTokenMask(oldToken), "current_token", adminTokenMask(newToken), "pool", pool)
	}
	return map[string]any{"status": "success", "token": newToken, "pool": pool}, nil
}

func adminTokensToggle(r *http.Request, repo adminTokensRepository, req adminTokensToggleRequest) (map[string]any, error) {
	token := adminTokenSanitize(req.Token)
	if token == "" {
		return nil, adminValidation("Token is required", "token")
	}
	records, err := repo.GetAccounts(r.Context(), []string{token})
	if err != nil || len(records) == 0 {
		return nil, adminAccountNotFound()
	}
	patch := adminTokensTogglePatch(records[0], req.Disabled)
	if _, err := repo.PatchAccounts(r.Context(), []adminBatchAccountPatch{patch}); err != nil {
		return nil, err
	}
	if req.Disabled {
		logging.Logger.Info("admin token disabled", "token", adminTokenMask(token))
	} else {
		logging.Logger.Info("admin token restored", "token", adminTokenMask(token))
	}
	return map[string]any{"status": "success", "token": token, "disabled": req.Disabled}, nil
}

func adminTokensToggleBatch(r *http.Request, repo adminTokensRepository, req adminTokensToggleBatchRequest) (map[string]any, error) {
	tokens := adminTokenDedupe(req.Tokens)
	if len(tokens) == 0 {
		return nil, adminValidation("No valid tokens provided", "tokens")
	}
	records, err := repo.GetAccounts(r.Context(), tokens)
	if err != nil || len(records) == 0 {
		return nil, platform.NewAppError("No matching accounts found", platform.ErrorKindValidation, "account_not_found", 404, nil)
	}
	patches := make([]adminBatchAccountPatch, 0, len(records))
	for _, record := range records {
		patches = append(patches, adminTokensTogglePatch(record, req.Disabled))
	}
	result, err := repo.PatchAccounts(r.Context(), patches)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "success", "disabled": req.Disabled, "summary": map[string]any{"total": len(tokens), "ok": result.Patched, "fail": maxAdminBatchInt(0, len(tokens)-result.Patched)}}, nil
}
