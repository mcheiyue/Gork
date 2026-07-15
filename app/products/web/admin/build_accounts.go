package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
)

// buildAccountAdminStore 是 Build 账号管理面（与 openai 选号目录同源实现）。
type buildAccountAdminStore interface {
	List(ctx context.Context) ([]buildaccount.Account, error)
	Get(ctx context.Context, id int64) (buildaccount.Account, error)
	Upsert(ctx context.Context, account buildaccount.Account) (buildaccount.Account, error)
	Delete(ctx context.Context, id int64) error
	SetStatus(ctx context.Context, id int64, status string, reason string) error
	UpdateBilling(ctx context.Context, id int64, billing build.Billing) error
	UpdateTokens(ctx context.Context, id int64, access, refresh string, expiresAt time.Time) error
}

var adminBuildAccountStore = func() buildAccountAdminStore { return nil }

// SetBuildAccountStore 注入 Build 账号仓储；返回恢复函数。
func SetBuildAccountStore(store buildAccountAdminStore) func() {
	previous := adminBuildAccountStore
	adminBuildAccountStore = func() buildAccountAdminStore { return store }
	return func() { adminBuildAccountStore = previous }
}

func requireBuildAccountStore() (buildAccountAdminStore, error) {
	store := adminBuildAccountStore()
	if store == nil {
		return nil, platform.NewAppError(
			"Build account store not initialised",
			platform.ErrorKindServer,
			"build_store_not_initialised",
			http.StatusServiceUnavailable,
			nil,
		)
	}
	return store, nil
}

func handleAdminBuildAccountsList(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	accounts, err := store.List(r.Context())
	if err != nil {
		writeAdminError(w, err)
		return
	}
	filter := parseBuildAccountListFilter(r)
	now := time.Now().UTC()
	items := make([]map[string]any, 0, len(accounts))
	for _, acc := range accounts {
		if !matchBuildAccountFilter(acc, filter, now) {
			continue
		}
		items = append(items, serializeBuildAccount(acc))
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"accounts": items,
		"total":    len(items),
		"facets":   buildAccountFacets(accounts, now),
	})
}

func handleAdminBuildAccountsImport(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	creds, err := build.ParseCredentials(raw)
	if err != nil {
		writeAdminError(w, platform.NewValidationError(err.Error(), "body", "invalid_credentials"))
		return
	}
	imported := make([]map[string]any, 0, len(creds))
	for _, cred := range creds {
		acc, err := store.Upsert(r.Context(), buildaccount.FromCredential(cred))
		if err != nil {
			writeAdminError(w, err)
			return
		}
		imported = append(imported, serializeBuildAccount(acc))
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"status":   "success",
		"imported": len(imported),
		"accounts": imported,
	})
}

func handleAdminBuildAccountsDelete(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	id, err := parseBuildAccountID(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	if err := store.Delete(r.Context(), id); err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "id": id})
}

func handleAdminBuildAccountsStatus(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, platform.NewValidationError("Invalid JSON body", "body", "invalid_json"))
		return
	}
	if req.ID <= 0 {
		writeAdminError(w, platform.NewValidationError("id is required", "id", ""))
		return
	}
	status := strings.TrimSpace(req.Status)
	switch status {
	case buildaccount.StatusActive, buildaccount.StatusDisabled, buildaccount.StatusCooling, buildaccount.StatusExpired:
	default:
		writeAdminError(w, platform.NewValidationError("invalid status", "status", ""))
		return
	}
	if err := store.SetStatus(r.Context(), req.ID, status, strings.TrimSpace(req.Reason)); err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "id": req.ID, "account_status": status})
}

func parseBuildAccountID(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	if raw == "" {
		return 0, platform.NewValidationError("id is required", "id", "")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, platform.NewValidationError("id must be a positive integer", "id", "")
	}
	return id, nil
}

// serializeBuildAccount 不回传明文 token，仅暴露是否已配置。
func serializeBuildAccount(acc buildaccount.Account) map[string]any {
	item := map[string]any{
		"id":                acc.ID,
		"name":              acc.Name,
		"email":             acc.Email,
		"user_id":           acc.UserID,
		"client_id":         acc.ClientID,
		"status":            acc.Status,
		"priority":          acc.Priority,
		"fail_count":        acc.FailCount,
		"has_access_token":  acc.AccessToken != "",
		"has_refresh_token": acc.RefreshToken != "",
	}
	if !acc.ExpiresAt.IsZero() {
		item["expires_at"] = acc.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if !acc.CoolingUntil.IsZero() {
		item["cooling_until"] = acc.CoolingUntil.UTC().Format(time.RFC3339)
	}
	if !acc.LastUseAt.IsZero() {
		item["last_use_at"] = acc.LastUseAt.UTC().Format(time.RFC3339)
	}
	if !acc.CreatedAt.IsZero() {
		item["created_at"] = acc.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !acc.UpdatedAt.IsZero() {
		item["updated_at"] = acc.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if hasBuildBillingSnapshot(acc) {
		item["billing"] = acc.Billing
		if !acc.BillingSynced.IsZero() {
			item["billing_synced_at"] = acc.BillingSynced.UTC().Format(time.RFC3339)
		} else if !acc.Billing.SyncedAt.IsZero() {
			item["billing_synced_at"] = acc.Billing.SyncedAt.UTC().Format(time.RFC3339)
		}
	}
	return item
}

// hasBuildBillingSnapshot 含 free 号零额度与仅账期字段的合法同步结果。
func hasBuildBillingSnapshot(acc buildaccount.Account) bool {
	if !acc.BillingSynced.IsZero() || !acc.Billing.SyncedAt.IsZero() {
		return true
	}
	b := acc.Billing
	return b.PlanCode != "" ||
		b.PlanName != "" ||
		b.IsUnifiedBillingUser ||
		b.TopUpMethod != "" ||
		b.BillingPeriodStart != "" ||
		b.BillingPeriodEnd != "" ||
		b.UsagePeriodType != "" ||
		b.UsagePeriodStart != "" ||
		b.UsagePeriodEnd != "" ||
		b.MonthlyLimit > 0 ||
		b.Used > 0 ||
		b.OnDemandCap > 0 ||
		b.OnDemandUsed > 0 ||
		b.PrepaidBalance > 0 ||
		b.CreditUsagePercent > 0
}
