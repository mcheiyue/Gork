package admin

import (
	"net/http"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	"github.com/dslzl/gork/app/platform"
)

func adminTokensNewOnly(tokens []string, records []adminAssetsAccount) []string {
	existing := map[string]struct{}{}
	for _, record := range records {
		if !record.Deleted && record.DeletedAt == nil {
			existing[record.Token] = struct{}{}
		}
	}
	out := []string{}
	for _, token := range tokens {
		if _, ok := existing[token]; !ok {
			out = append(out, token)
		}
	}
	return out
}

func adminTokensUpserts(tokens []string, pool string, tags []string) []adminTokensUpsert {
	upserts := make([]adminTokensUpsert, 0, len(tokens))
	for _, token := range tokens {
		upserts = append(upserts, adminTokensUpsert{Token: token, Pool: pool, Tags: tags})
	}
	return upserts
}

func adminTokensUpserted(result adminTokensPatchResult, fallback int) int {
	if result.Upserted > 0 {
		return result.Upserted
	}
	return fallback
}

func adminTokenMask(token string) string {
	if len(token) <= 20 {
		return token
	}
	return token[:8] + "..." + token[len(token)-8:]
}

func adminTokensEnsureTargetFree(r *http.Request, repo adminTokensRepository, token string) error {
	existing, err := repo.GetAccounts(r.Context(), []string{token})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return platform.NewAppError("Target token already exists", platform.ErrorKindValidation, "token_conflict", 409, nil)
	}
	return nil
}

func adminTokensCopyStateAndDeleteOld(r *http.Request, repo adminTokensRepository, oldToken string, newToken string, pool string, record adminAssetsAccount) error {
	patch := adminTokensEditPatch(newToken, pool, record)
	patch.Tags = append([]string(nil), record.Tags...)
	patch.ExtMerge = cloneRuntimeMap(record.Ext)
	if _, err := repo.PatchAccounts(r.Context(), []adminBatchAccountPatch{patch}); err != nil {
		return err
	}
	_, err := repo.DeleteAccounts(r.Context(), []string{oldToken})
	return err
}

func adminTokensEditPatch(token string, pool string, record adminAssetsAccount) adminBatchAccountPatch {
	patch := adminBatchAccountPatch{
		Token: token, Pool: pool, Status: record.Status,
		UsageUseDelta: record.UsageUseCount, UsageFailDelta: record.UsageFailCount,
		LastUseAt: record.LastUseAt, LastFailAt: record.LastFailAt, LastFailReason: record.LastFailReason,
		LastSyncAt: record.LastSyncAt, LastClearAt: record.LastClearAt, StateReason: record.StateReason,
	}
	quotaSet, err := accountcontrol.AccountQuotaSetFromDict(record.Quota)
	if err != nil {
		quotaSet = accountcontrol.DefaultQuotaSet(pool)
	}
	quota := accountcontrol.NormalizeQuotaSet(pool, quotaSet).ToDict()
	patch.QuotaAuto = adminQuotaWindowPatch(quota, "auto")
	patch.QuotaFast = adminQuotaWindowPatch(quota, "fast")
	patch.QuotaExpert = adminQuotaWindowPatch(quota, "expert")
	patch.QuotaHeavy = adminQuotaWindowPatch(quota, "heavy")
	patch.QuotaGrok43 = adminQuotaWindowPatch(quota, "grok_4_3")
	patch.QuotaConsole = adminQuotaWindowPatch(quota, "console")
	return patch
}

func adminQuotaWindowPatch(quota map[string]any, key string) map[string]any {
	window, ok := quota[key].(map[string]any)
	if !ok {
		return nil
	}
	return cloneRuntimeMap(window)
}

func adminTokensTogglePatch(record adminAssetsAccount, disabled bool) adminBatchAccountPatch {
	if disabled {
		ext := cloneAdminMap(record.Ext)
		ext["disabled_at"] = adminTokensNowMS()
		ext["disabled_reason"] = "operator_disabled"
		return adminBatchAccountPatch{Token: record.Token, Status: "disabled", StateReason: "operator_disabled", ExtMerge: ext}
	}
	return adminBatchAccountPatch{Token: record.Token, Status: "active", ClearFailures: true}
}

func adminAccountNotFound() error {
	return platform.NewAppError("Account not found", platform.ErrorKindValidation, "account_not_found", 404, nil)
}
