package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

// buildBillingFetcher 可注入，便于单测。
type buildBillingFetcher interface {
	GetBilling(ctx context.Context, accessToken string) (build.Billing, error)
}

var adminBuildBillingFetcher = func() buildBillingFetcher {
	cfg := build.ClientConfig{
		BaseURL:          platformconfig.GlobalConfig.GetStr("provider.build.base_url", build.DefaultBaseURL),
		ClientVersion:    platformconfig.GlobalConfig.GetStr("provider.build.client_version", build.DefaultClientVersion),
		ClientIdentifier: platformconfig.GlobalConfig.GetStr("provider.build.client_identifier", build.DefaultClientIDName),
		TokenAuth:        platformconfig.GlobalConfig.GetStr("provider.build.token_auth", build.DefaultTokenAuth),
		UserAgent:        platformconfig.GlobalConfig.GetStr("provider.build.user_agent", build.DefaultUserAgent),
		Timeout:          time.Duration(platformconfig.GlobalConfig.GetFloat("provider.build.timeout_seconds", 120)) * time.Second,
	}
	return build.NewAPIClient(nil, cfg)
}

// handleAdminBuildAccountsBilling POST {id} 拉取上游 Billing 并落库。
func handleAdminBuildAccountsBilling(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, platform.NewValidationError("Invalid JSON body", "body", "invalid_json"))
		return
	}
	if req.ID <= 0 {
		writeAdminError(w, platform.NewValidationError("id is required", "id", ""))
		return
	}
	acc, err := store.Get(r.Context(), req.ID)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	billing, err := fetchBuildBillingWithRefresh(r.Context(), store, acc)
	if err != nil {
		// 失败不阻断主路径语义：管理面返回错误，但不当作账号永久失效
		writeAdminError(w, err)
		return
	}
	if err := store.UpdateBilling(r.Context(), acc.ID, billing); err != nil {
		writeAdminError(w, err)
		return
	}
	// 回读最新 token/计费字段
	if latest, gerr := store.Get(r.Context(), acc.ID); gerr == nil {
		acc = latest
	} else {
		acc.Billing = billing
		acc.BillingSynced = billing.SyncedAt
		if acc.BillingSynced.IsZero() {
			acc.BillingSynced = time.Now().UTC()
		}
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"id":      acc.ID,
		"billing": billing,
		"account": serializeBuildAccount(acc),
	})
}

// fetchBuildBillingWithRefresh 过期先 refresh；GetBilling 401 再 refresh 重试一次。
func fetchBuildBillingWithRefresh(ctx context.Context, store buildAccountAdminStore, acc buildaccount.Account) (build.Billing, error) {
	access, acc, err := ensureBuildAccessToken(ctx, store, acc)
	if err != nil {
		return build.Billing{}, err
	}
	billing, err := adminBuildBillingFetcher().GetBilling(ctx, access)
	if err == nil {
		return billing, nil
	}
	if !build.IsUnauthorized(err) || strings.TrimSpace(acc.RefreshToken) == "" {
		return build.Billing{}, err
	}
	if rerr := refreshOneBuildAccount(ctx, store, acc); rerr != nil {
		return build.Billing{}, err
	}
	latest, gerr := store.Get(ctx, acc.ID)
	if gerr != nil {
		return build.Billing{}, err
	}
	access = strings.TrimSpace(latest.AccessToken)
	if access == "" {
		return build.Billing{}, err
	}
	return adminBuildBillingFetcher().GetBilling(ctx, access)
}

// ensureBuildAccessToken 需要时 refresh，返回可用 access。
func ensureBuildAccessToken(ctx context.Context, store buildAccountAdminStore, acc buildaccount.Account) (string, buildaccount.Account, error) {
	access := strings.TrimSpace(acc.AccessToken)
	if access == "" && strings.TrimSpace(acc.RefreshToken) == "" {
		return "", acc, platform.NewValidationError("account has no tokens", "access_token", "")
	}
	if acc.NeedsRefresh(time.Now().UTC(), 2*time.Minute) && strings.TrimSpace(acc.RefreshToken) != "" {
		if err := refreshOneBuildAccount(ctx, store, acc); err == nil {
			if latest, gerr := store.Get(ctx, acc.ID); gerr == nil {
				acc = latest
				access = strings.TrimSpace(latest.AccessToken)
			}
		}
	}
	if access == "" {
		return "", acc, platform.NewValidationError("account has no access_token", "access_token", "")
	}
	return access, acc, nil
}

func firstNonEmptyAdmin(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
