package admin

import (
	"context"

	"github.com/dslzl/gork/app/platform/config"
)

type adminBatchRepository interface {
	adminAssetsRepository
	PatchAccounts(context.Context, []adminBatchAccountPatch) (adminTokensPatchResult, error)
}

type adminBatchAccountPatch struct {
	Token          string
	Status         string
	Tags           []string
	AddTags        []string
	RemoveTags     []string
	ClearFailures  bool
	StateReason    string
	ExtMerge       map[string]any
	QuotaAuto      map[string]any
	QuotaFast      map[string]any
	QuotaExpert    map[string]any
	UsageUseDelta  int
	UsageFailDelta int
	UsageSyncDelta int
	LastUseAt      int64
	LastFailAt     int64
	LastFailReason string
	LastSyncAt     int64
	LastClearAt    int64
}

type adminBatchRefreshService interface {
	RefreshTokens(context.Context, []string) (adminBatchRefreshResult, error)
}

type adminBatchRefreshResult struct {
	Refreshed int
	Expired   int
	Failed    int
}

type adminBatchRequest struct {
	Tokens []string `json:"tokens"`
}

type adminBatchHandler func(context.Context, string) (map[string]any, error)

type adminBatchItemResult struct {
	Token      string
	Data       map[string]any
	Error      string
	ErrorClass string
}

var (
	adminBatchRepoProvider           = defaultAdminBatchRepoProvider
	adminBatchRefreshServiceProvider = func() adminBatchRefreshService {
		return nil
	}
	adminBatchConfigInt = func(key string, fallback int) int {
		if adminRouterConfig != nil {
			return adminRouterConfig.GetInt(key, fallback)
		}
		return config.GlobalConfig.GetInt(key, fallback)
	}
	adminBatchAsyncRunner  = func(run func()) { go run() }
	adminBatchNSFWSequence = defaultAdminBatchNSFWSequence
	adminBatchSetNSFW      = defaultAdminBatchSetNSFW
)

func defaultAdminBatchRepoProvider() adminBatchRepository {
	if repo, ok := adminAssetsRepoProvider().(adminBatchRepository); ok {
		return repo
	}
	if repo, ok := adminAccountDirectory().(adminBatchRepository); ok {
		return repo
	}
	return nil
}
