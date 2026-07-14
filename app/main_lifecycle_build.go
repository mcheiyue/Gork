package app

import (
	"context"
	"fmt"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
	"github.com/dslzl/gork/app/platform/security"
	openaiproduct "github.com/dslzl/gork/app/products/openai"
	adminproduct "github.com/dslzl/gork/app/products/web/admin"
)

// buildAccountsDBFile 与 data 目录并列的独立 SQLite（不进 SSO accounts）。
const buildAccountsDBFile = "build_accounts.db"

// defaultAppMainInitializeBuildAccountStore 打开 Build 账号库并注入 openai 产品选号目录。
// 关 features.build_provider 时仍挂载空池：请求路径被门闸拦住，B-c admin 可先导入。
func defaultAppMainInitializeBuildAccountStore(ctx context.Context, state *appMainLifecycleState) (Hook, error) {
	cipher, err := security.OpenCipher(platformconfig.GlobalConfig.GetStr("security.credential_encryption_key", ""))
	if err != nil {
		return nil, fmt.Errorf("build account cipher: %w", err)
	}
	path := platform.DataPath(buildAccountsDBFile)
	store := buildaccount.NewSQLiteStore(path, cipher)
	if err := store.Initialize(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("build account store: %w", err)
	}
	state.buildAccountStore = store
	openaiproduct.SetBuildAccountDirectory(store)
	restoreAdmin := adminproduct.SetBuildAccountStore(store)
	return func(context.Context) error {
		restoreAdmin()
		openaiproduct.SetBuildAccountDirectory(nil)
		state.buildAccountStore = nil
		return store.Close()
	}, nil
}
