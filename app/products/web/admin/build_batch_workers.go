package admin

import (
	"context"
	"strings"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	runtimepkg "github.com/dslzl/gork/app/platform/runtime"
)

func runBuildBillingBatch(ctx context.Context, store buildAccountAdminStore, task *runtimepkg.AsyncTask, targets []buildaccount.Account) {
	defer goExpireBuildTask(task.ID)
	ok, fail := 0, 0
	for _, acc := range targets {
		if task.IsCancelled() {
			task.FinishCancelled()
			return
		}
		if err := syncOneBuildBilling(ctx, store, acc); err != nil {
			fail++
			task.Record(false, runtimepkg.TaskRecordOptions{Count: 1, Error: err.Error(), Item: acc.ID})
			continue
		}
		ok++
		task.Record(true, runtimepkg.TaskRecordOptions{Count: 1, Item: acc.ID, Detail: map[string]any{"ok": ok, "fail": fail}})
	}
	task.Finish(map[string]any{"status": "success", "summary": map[string]any{"total": len(targets), "ok": ok, "fail": fail}})
}

func runBuildRefreshBatch(ctx context.Context, store buildAccountAdminStore, task *runtimepkg.AsyncTask, targets []buildaccount.Account) {
	defer goExpireBuildTask(task.ID)
	ok, fail, skipped := 0, 0, 0
	for _, acc := range targets {
		if task.IsCancelled() {
			task.FinishCancelled()
			return
		}
		if strings.TrimSpace(acc.RefreshToken) == "" {
			skipped++
			fail++
			task.Record(false, runtimepkg.TaskRecordOptions{Count: 1, Error: "no refresh_token", Item: acc.ID})
			continue
		}
		if err := refreshOneBuildAccount(ctx, store, acc); err != nil {
			fail++
			task.Record(false, runtimepkg.TaskRecordOptions{Count: 1, Error: err.Error(), Item: acc.ID})
			continue
		}
		ok++
		task.Record(true, runtimepkg.TaskRecordOptions{Count: 1, Item: acc.ID, Detail: map[string]any{"ok": ok, "fail": fail, "skipped": skipped}})
	}
	task.Finish(map[string]any{"status": "success", "summary": map[string]any{"total": len(targets), "ok": ok, "fail": fail, "skipped": skipped}})
}

func runBuildCleanupBatch(ctx context.Context, store buildAccountAdminStore, task *runtimepkg.AsyncTask, targets []buildaccount.Account, mode string) {
	defer goExpireBuildTask(task.ID)
	ok, fail := 0, 0
	for _, acc := range targets {
		if task.IsCancelled() {
			task.FinishCancelled()
			return
		}
		if err := store.Delete(ctx, acc.ID); err != nil {
			fail++
			task.Record(false, runtimepkg.TaskRecordOptions{Count: 1, Error: err.Error(), Item: acc.ID})
			continue
		}
		ok++
		task.Record(true, runtimepkg.TaskRecordOptions{Count: 1, Item: acc.ID, Detail: map[string]any{"mode": mode, "deleted": ok}})
	}
	task.Finish(map[string]any{"status": "success", "summary": map[string]any{"mode": mode, "total": len(targets), "ok": ok, "deleted": ok, "fail": fail}})
}

func syncOneBuildBilling(ctx context.Context, store buildAccountAdminStore, acc buildaccount.Account) error {
	billing, err := fetchBuildBillingWithRefresh(ctx, store, acc)
	if err != nil {
		return err
	}
	return store.UpdateBilling(ctx, acc.ID, billing)
}

func refreshOneBuildAccount(ctx context.Context, store buildAccountAdminStore, acc buildaccount.Account) error {
	if strings.TrimSpace(acc.RefreshToken) == "" {
		return platform.NewValidationError("account has no refresh_token", "refresh_token", "token_missing")
	}
	oauth := build.NewOAuthClient(nil, build.OAuthConfig{
		ClientID:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_client_id", build.DefaultOAuthClientID),
		Scope:     platformconfig.GlobalConfig.GetStr("provider.build.oauth_scope", build.DefaultOAuthScope),
		DeviceURL: platformconfig.GlobalConfig.GetStr("provider.build.oauth_device_url", build.DefaultDeviceURL),
		TokenURL:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_token_url", build.DefaultTokenURL),
	})
	tok, err := oauth.Refresh(ctx, acc.RefreshToken)
	if err != nil {
		if build.IsPermanentRefresh(err) {
			_ = store.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
		}
		return err
	}
	return store.UpdateTokens(ctx, acc.ID, tok.AccessToken,
		firstNonEmptyAdmin(tok.RefreshToken, acc.RefreshToken), tok.ExpiresAt)
}
