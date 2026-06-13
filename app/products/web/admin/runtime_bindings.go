package admin

import (
	"context"

	accountcontrol "github.com/dslzl/gork/app/control/account"
	accountdataplane "github.com/dslzl/gork/app/dataplane/account"
)

type accountRuntimeRepository struct {
	repo accountcontrol.AccountRepository
}

type accountRuntimeRefreshService struct {
	service *accountcontrol.AccountRefreshService
}

type accountFacetRepository interface {
	ListFacets(context.Context) (accountcontrol.AccountFacetSnapshot, error)
}

func BindAccountRuntime(
	repo accountcontrol.AccountRepository,
	directory *accountdataplane.AccountDirectory,
	refreshService *accountcontrol.AccountRefreshService,
) func() {
	previousAssetsRepo := adminAssetsRepoProvider
	previousDirectory := adminAccountDirectory
	previousBatchRefresh := adminBatchRefreshServiceProvider
	previousTokensRefresh := adminTokensRefreshServiceProvider

	if repo != nil {
		adapter := accountRuntimeRepository{repo: repo}
		adminAssetsRepoProvider = func() adminAssetsRepository { return adapter }
	}
	if directory != nil {
		adminAccountDirectory = func() adminDirectory { return directory }
	}
	if refreshService != nil {
		adapter := accountRuntimeRefreshService{service: refreshService}
		adminBatchRefreshServiceProvider = func() adminBatchRefreshService { return adapter }
		adminTokensRefreshServiceProvider = func() adminTokensRefreshService { return adapter }
	}
	return func() {
		adminAssetsRepoProvider = previousAssetsRepo
		adminAccountDirectory = previousDirectory
		adminBatchRefreshServiceProvider = previousBatchRefresh
		adminTokensRefreshServiceProvider = previousTokensRefresh
	}
}

func (r accountRuntimeRepository) ListAccounts(ctx context.Context, query adminAssetsListQuery) (adminAssetsListResult, error) {
	status := accountStatusPtr(query.Status)
	page, err := r.repo.ListAccounts(ctx, accountcontrol.ListAccountsQuery{
		Page:        query.Page,
		PageSize:    query.PageSize,
		Pool:        stringPtr(query.Pool),
		Status:      status,
		Tags:        append([]string(nil), query.Tags...),
		ExcludeTags: append([]string(nil), query.ExcludeTags...),
		SortBy:      query.SortBy,
		SortDesc:    query.SortDesc,
	})
	if err != nil {
		return adminAssetsListResult{}, err
	}
	return adminAssetsListResult{
		Items:      adminAccountsFromControl(page.Items),
		Total:      page.Total,
		Page:       page.Page,
		PageSize:   page.PageSize,
		TotalPages: page.TotalPages,
		Revision:   int64(page.Revision),
	}, nil
}

func (r accountRuntimeRepository) PatchAccounts(ctx context.Context, patches []adminBatchAccountPatch) (adminTokensPatchResult, error) {
	result, err := r.repo.PatchAccounts(ctx, accountPatchesFromAdmin(patches))
	return adminPatchResultFromControl(result), err
}

func (r accountRuntimeRepository) GetAccounts(ctx context.Context, tokens []string) ([]adminAssetsAccount, error) {
	records, err := r.repo.GetAccounts(ctx, tokens)
	if err != nil {
		return nil, err
	}
	return adminAccountsFromControl(records), nil
}

func (r accountRuntimeRepository) ListFacets(ctx context.Context) (adminTokensFacetSnapshot, error) {
	if facetRepo, ok := r.repo.(accountFacetRepository); ok {
		facets, err := facetRepo.ListFacets(ctx)
		if err != nil {
			return adminTokensFacetSnapshot{}, err
		}
		return adminFacetSnapshotFromControl(facets), nil
	}
	records, err := r.collectAllAccounts(ctx)
	if err != nil {
		return adminTokensFacetSnapshot{}, err
	}
	return adminTokensFacetSnapshotFromRecords(records), nil
}

func (r accountRuntimeRepository) collectAllAccounts(ctx context.Context) ([]adminAssetsAccount, error) {
	items := []adminAssetsAccount{}
	for page := 1; ; page++ {
		result, err := r.ListAccounts(ctx, adminAssetsListQuery{Page: page, PageSize: adminAssetsPageSize})
		if err != nil {
			return nil, err
		}
		items = append(items, result.Items...)
		if adminAssetsLastPage(page, result) {
			return items, nil
		}
	}
}

func (r accountRuntimeRepository) UpsertAccounts(ctx context.Context, upserts []adminTokensUpsert) (adminTokensPatchResult, error) {
	result, err := r.repo.UpsertAccounts(ctx, accountUpsertsFromAdmin(upserts))
	return adminPatchResultFromControl(result), err
}

func (r accountRuntimeRepository) DeleteAccounts(ctx context.Context, tokens []string) (adminTokensPatchResult, error) {
	result, err := r.repo.DeleteAccounts(ctx, tokens)
	return adminPatchResultFromControl(result), err
}

func (r accountRuntimeRepository) ReplacePool(ctx context.Context, command adminTokensReplacePoolCommand) (adminTokensPatchResult, error) {
	result, err := r.repo.ReplacePool(ctx, accountcontrol.BulkReplacePoolCommand{
		Pool:    command.Pool,
		Upserts: accountUpsertsFromAdmin(command.Upserts),
	})
	return adminPatchResultFromControl(result), err
}

func (s accountRuntimeRefreshService) RefreshTokens(ctx context.Context, tokens []string) (adminBatchRefreshResult, error) {
	result, err := s.service.RefreshTokens(ctx, tokens)
	return adminBatchRefreshResult{Refreshed: result.Refreshed, Expired: result.Expired, Failed: result.Failed}, err
}

func (s accountRuntimeRefreshService) RefreshOnImport(ctx context.Context, tokens []string) (adminTokensRefreshResult, error) {
	result, err := s.service.RefreshOnImport(ctx, tokens)
	return adminTokensRefreshResult{Refreshed: result.Refreshed, Failed: result.Failed}, err
}

func adminAccountsFromControl(records []accountcontrol.AccountRecord) []adminAssetsAccount {
	out := make([]adminAssetsAccount, 0, len(records))
	for _, record := range records {
		out = append(out, adminAccountFromControl(record))
	}
	return out
}

func adminFacetSnapshotFromControl(snapshot accountcontrol.AccountFacetSnapshot) adminTokensFacetSnapshot {
	return adminTokensFacetSnapshot{
		Status: cloneIntMap(snapshot.Status),
		NSFW:   cloneIntMap(snapshot.NSFW),
		Pools:  cloneIntMap(snapshot.Pools),
		Stats:  cloneIntMap(snapshot.Stats),
	}
}

func cloneIntMap(values map[string]int) map[string]int {
	out := map[string]int{}
	for key, value := range values {
		out[key] = value
	}
	return out
}

func adminAccountFromControl(record accountcontrol.AccountRecord) adminAssetsAccount {
	return adminAssetsAccount{
		Token:          record.Token,
		Pool:           record.Pool,
		Status:         string(record.Status),
		Tags:           append([]string(nil), record.Tags...),
		Quota:          cloneRuntimeMap(record.Quota),
		UsageUseCount:  record.UsageUseCount,
		UsageFailCount: record.UsageFailCount,
		LastUseAt:      int64Value(record.LastUseAt),
		LastFailAt:     int64Value(record.LastFailAt),
		LastFailReason: stringValue(record.LastFailReason),
		LastSyncAt:     int64Value(record.LastSyncAt),
		LastClearAt:    int64Value(record.LastClearAt),
		StateReason:    stringValue(record.StateReason),
		Ext:            cloneRuntimeMap(record.Ext),
		Deleted:        record.DeletedAt != nil,
		DeletedAt:      anyInt64Value(record.DeletedAt),
	}
}

func accountUpsertsFromAdmin(upserts []adminTokensUpsert) []accountcontrol.AccountUpsert {
	out := make([]accountcontrol.AccountUpsert, 0, len(upserts))
	for _, upsert := range upserts {
		out = append(out, accountcontrol.AccountUpsert{
			Token: upsert.Token,
			Pool:  upsert.Pool,
			Tags:  append([]string(nil), upsert.Tags...),
			Ext:   cloneRuntimeMap(upsert.Ext),
		})
	}
	return out
}

func accountPatchesFromAdmin(patches []adminBatchAccountPatch) []accountcontrol.AccountPatch {
	out := make([]accountcontrol.AccountPatch, 0, len(patches))
	for _, patch := range patches {
		out = append(out, accountPatchFromAdmin(patch))
	}
	return out
}

func accountPatchFromAdmin(patch adminBatchAccountPatch) accountcontrol.AccountPatch {
	status := accountStatusPtr(patch.Status)
	return accountcontrol.AccountPatch{
		Token:          patch.Token,
		Status:         status,
		Tags:           append([]string(nil), patch.Tags...),
		AddTags:        append([]string(nil), patch.AddTags...),
		RemoveTags:     append([]string(nil), patch.RemoveTags...),
		QuotaAuto:      cloneRuntimeMap(patch.QuotaAuto),
		QuotaFast:      cloneRuntimeMap(patch.QuotaFast),
		QuotaExpert:    cloneRuntimeMap(patch.QuotaExpert),
		UsageUseDelta:  intPtrIfNonZero(patch.UsageUseDelta),
		UsageFailDelta: intPtrIfNonZero(patch.UsageFailDelta),
		UsageSyncDelta: intPtrIfNonZero(patch.UsageSyncDelta),
		LastUseAt:      int64PtrIfNonZero(patch.LastUseAt),
		LastFailAt:     int64PtrIfNonZero(patch.LastFailAt),
		LastFailReason: stringPtr(patch.LastFailReason),
		LastSyncAt:     int64PtrIfNonZero(patch.LastSyncAt),
		LastClearAt:    int64PtrIfNonZero(patch.LastClearAt),
		StateReason:    stringPtr(patch.StateReason),
		ExtMerge:       cloneRuntimeMap(patch.ExtMerge),
		ClearFailures:  patch.ClearFailures,
	}
}

func adminPatchResultFromControl(result accountcontrol.AccountMutationResult) adminTokensPatchResult {
	return adminTokensPatchResult{
		Upserted: result.Upserted,
		Patched:  result.Patched,
		Deleted:  result.Deleted,
	}
}

func accountStatusPtr(value string) *accountcontrol.AccountStatus {
	if value == "" {
		return nil
	}
	status := accountcontrol.AccountStatus(value)
	return &status
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intPtrIfNonZero(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func int64PtrIfNonZero(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func anyInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneRuntimeMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
