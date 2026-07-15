package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		writeAdminError(w, mapBuildAccountNotFound(err))
		return
	}
	billing, err := fetchBuildBillingWithRefresh(r.Context(), store, acc)
	if err != nil {
		writeAdminError(w, mapBuildAdminError(err))
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

// fetchBuildBillingWithRefresh 过期必须 refresh 成功后再拉配额；401 再 refresh 一次。
// 失败返回可读 4xx/502 错误，禁止用死 access 打上游后落到 500。
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
		return build.Billing{}, mapBuildUpstreamError(err)
	}
	// 401：强制再 refresh 一次（可能 NeedsRefresh 阈值内但 token 已吊销）
	if rerr := refreshOneBuildAccount(ctx, store, acc); rerr != nil {
		return build.Billing{}, mapBuildTokenError(ctx, store, acc.ID, rerr)
	}
	latest, gerr := store.Get(ctx, acc.ID)
	if gerr != nil {
		return build.Billing{}, mapBuildAccountNotFound(gerr)
	}
	access = strings.TrimSpace(latest.AccessToken)
	if access == "" {
		return build.Billing{}, platform.NewValidationError(
			"Build account has no access_token after refresh",
			"access_token",
			"token_missing",
		)
	}
	billing, err = adminBuildBillingFetcher().GetBilling(ctx, access)
	if err != nil {
		return build.Billing{}, mapBuildUpstreamError(err)
	}
	return billing, nil
}

// ensureBuildAccessToken 需要时必须 refresh 成功；失败标 expired（永久）并返回错误，不回传死 token。
func ensureBuildAccessToken(ctx context.Context, store buildAccountAdminStore, acc buildaccount.Account) (string, buildaccount.Account, error) {
	access := strings.TrimSpace(acc.AccessToken)
	refresh := strings.TrimSpace(acc.RefreshToken)
	if access == "" && refresh == "" {
		return "", acc, platform.NewValidationError("account has no tokens", "access_token", "token_missing")
	}
	needsRefresh := acc.NeedsRefresh(time.Now().UTC(), 2*time.Minute) || access == ""
	if !needsRefresh {
		return access, acc, nil
	}
	if refresh == "" {
		return "", acc, platform.NewValidationError(
			"Build access token expired and no refresh_token",
			"refresh_token",
			"token_expired",
		)
	}
	if err := refreshOneBuildAccount(ctx, store, acc); err != nil {
		return "", acc, mapBuildTokenError(ctx, store, acc.ID, err)
	}
	latest, gerr := store.Get(ctx, acc.ID)
	if gerr != nil {
		return "", acc, mapBuildAccountNotFound(gerr)
	}
	access = strings.TrimSpace(latest.AccessToken)
	if access == "" {
		return "", latest, platform.NewValidationError(
			"Build account has no access_token after refresh",
			"access_token",
			"token_missing",
		)
	}
	return access, latest, nil
}

// mapBuildTokenError 将 OAuth refresh 失败转为 Admin 4xx，并标记永久失效账号。
func mapBuildTokenError(ctx context.Context, store buildAccountAdminStore, id int64, err error) error {
	if err == nil {
		return nil
	}
	if build.IsPermanentRefresh(err) {
		_ = store.SetStatus(ctx, id, buildaccount.StatusExpired, "refresh permanent failure")
		return platform.NewValidationError(
			fmt.Sprintf("Build refresh token invalid: %v", err),
			"refresh_token",
			"token_revoked",
		)
	}
	// 瞬时失败：429 等
	return platform.NewAppError(
		fmt.Sprintf("Build token refresh failed: %v", err),
		platform.ErrorKindUpstream,
		"token_refresh_failed",
		502,
		nil,
	)
}

func mapBuildUpstreamError(err error) error {
	if err == nil {
		return nil
	}
	var ue *build.UpstreamError
	if errors.As(err, &ue) {
		status := ue.Status
		if status == 401 {
			// 账号凭证问题，对 Admin 用 400 可读文案（避免浏览器当服务端崩溃）
			return platform.NewValidationError(
				fmt.Sprintf("Build upstream unauthorized (token invalid or expired): %s", truncateBody(ue.Body, 200)),
				"access_token",
				"upstream_unauthorized",
			)
		}
		if status < 400 {
			status = 502
		}
		if status >= 500 {
			status = 502
		}
		return platform.NewUpstreamError(
			fmt.Sprintf("Build upstream %s status=%d", firstNonEmptyAdmin(ue.Op, "request"), ue.Status),
			status,
			truncateBody(ue.Body, 500),
		)
	}
	return err
}

func mapBuildAccountNotFound(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return platform.NewValidationError(err.Error(), "id", "not_found")
	}
	return err
}

func mapBuildAdminError(err error) error {
	if err == nil {
		return nil
	}
	// 已是 platform 错误
	var ve *platform.ValidationError
	if errors.As(err, &ve) {
		return err
	}
	var ue *platform.UpstreamError
	if errors.As(err, &ue) {
		return err
	}
	var ae *platform.AppError
	if errors.As(err, &ae) {
		return err
	}
	if mapped := mapBuildUpstreamError(err); mapped != err {
		return mapped
	}
	return err
}

func truncateBody(body string, max int) string {
	body = strings.TrimSpace(body)
	if max <= 0 || len(body) <= max {
		return body
	}
	return body[:max] + "…"
}

func firstNonEmptyAdmin(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
