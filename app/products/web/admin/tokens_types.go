package admin

import (
	"context"
	"time"
)

const adminTokensImportChunkSize = 500

type adminTokensRepository interface {
	adminBatchRepository
	GetAccounts(context.Context, []string) ([]adminAssetsAccount, error)
	ListFacets(context.Context) (adminTokensFacetSnapshot, error)
	UpsertAccounts(context.Context, []adminTokensUpsert) (adminTokensPatchResult, error)
	DeleteAccounts(context.Context, []string) (adminTokensPatchResult, error)
	ReplacePool(context.Context, adminTokensReplacePoolCommand) (adminTokensPatchResult, error)
}

type adminTokensRefreshService interface {
	RefreshOnImport(context.Context, []string) (adminTokensRefreshResult, error)
}

type adminTokensRefreshResult struct {
	Refreshed int
	Failed    int
}

type adminTokensFacetSnapshot struct {
	Status map[string]int
	NSFW   map[string]int
	Pools  map[string]int
	Stats  map[string]int
}

type adminTokensUpsert struct {
	Token string
	Pool  string
	Tags  []string
	Ext   map[string]any
}

type adminTokensReplacePoolCommand struct {
	Pool    string
	Upserts []adminTokensUpsert
}

type adminTokensPatchResult struct {
	Upserted int
	Patched  int
	Deleted  int
}

var (
	adminTokensRepoProvider           = defaultAdminTokensRepoProvider
	adminTokensRefreshServiceProvider = func() adminTokensRefreshService {
		return nil
	}
	adminTokensAsyncRunner = func(run func()) { go run() }
	adminTokensNowMS       = defaultAdminTokensNowMS
)

func defaultAdminTokensRepoProvider() adminTokensRepository {
	if repo, ok := adminBatchRepoProvider().(adminTokensRepository); ok {
		return repo
	}
	if repo, ok := adminAccountDirectory().(adminTokensRepository); ok {
		return repo
	}
	return nil
}

func defaultAdminTokensNowMS() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
