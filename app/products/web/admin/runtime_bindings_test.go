package admin

import (
	"context"
	"testing"

	accountcontrol "github.com/jiujiu532/grok2api/app/control/account"
	accountdataplane "github.com/jiujiu532/grok2api/app/dataplane/account"
)

func TestBindAccountRuntimeWiresDefaultAdminProviders(t *testing.T) {
	resetAdminRouterDepsForTest(t)
	repo := &fakeAccountRuntimeRepo{
		page: accountcontrol.AccountPage{
			Items: []accountcontrol.AccountRecord{{
				Token:  "tok-live",
				Pool:   "basic",
				Status: accountcontrol.AccountStatusActive,
				Tags:   []string{"nsfw"},
			}},
			Total: 1, Page: 1, PageSize: 50, TotalPages: 1, Revision: 3,
		},
	}
	directory := accountdataplane.NewAccountDirectory(repo)
	cleanup := BindAccountRuntime(repo, directory, nil)
	t.Cleanup(cleanup)

	if adminAccountDirectory() != directory {
		t.Fatalf("admin directory provider was not wired")
	}
	provided := defaultAdminTokensRepoProvider()
	if provided == nil {
		t.Fatalf("default admin tokens repo provider returned nil")
	}
	result, err := provided.ListAccounts(context.Background(), adminAssetsListQuery{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Token != "tok-live" || result.Revision != 3 {
		t.Fatalf("admin list result = %#v", result)
	}
	if repo.queries[0].IncludeDeleted {
		t.Fatalf("admin runtime list should not include soft-deleted tokens: %#v", repo.queries[0])
	}

	cleanup()
	if adminAccountDirectory() != nil {
		t.Fatalf("cleanup did not restore directory provider")
	}
}

type fakeAccountRuntimeRepo struct {
	page    accountcontrol.AccountPage
	queries []accountcontrol.ListAccountsQuery
}

func (r *fakeAccountRuntimeRepo) Initialize(context.Context) error { return nil }

func (r *fakeAccountRuntimeRepo) GetRevision(context.Context) (int, error) {
	return int(r.page.Revision), nil
}

func (r *fakeAccountRuntimeRepo) RuntimeSnapshot(context.Context) (accountcontrol.RuntimeSnapshot, error) {
	return accountcontrol.RuntimeSnapshot{Revision: int(r.page.Revision), Items: r.page.Items}, nil
}

func (r *fakeAccountRuntimeRepo) ScanChanges(context.Context, int, int) (accountcontrol.AccountChangeSet, error) {
	return accountcontrol.NewAccountChangeSet(), nil
}

func (r *fakeAccountRuntimeRepo) UpsertAccounts(context.Context, []accountcontrol.AccountUpsert) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Upserted: 1}, nil
}

func (r *fakeAccountRuntimeRepo) PatchAccounts(context.Context, []accountcontrol.AccountPatch) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Patched: 1}, nil
}

func (r *fakeAccountRuntimeRepo) DeleteAccounts(context.Context, []string) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Deleted: 1}, nil
}

func (r *fakeAccountRuntimeRepo) GetAccounts(context.Context, []string) ([]accountcontrol.AccountRecord, error) {
	return r.page.Items, nil
}

func (r *fakeAccountRuntimeRepo) ListAccounts(_ context.Context, query accountcontrol.ListAccountsQuery) (accountcontrol.AccountPage, error) {
	r.queries = append(r.queries, query)
	return r.page, nil
}

func (r *fakeAccountRuntimeRepo) ReplacePool(context.Context, accountcontrol.BulkReplacePoolCommand) (accountcontrol.AccountMutationResult, error) {
	return accountcontrol.AccountMutationResult{Upserted: 1}, nil
}

func (r *fakeAccountRuntimeRepo) Close(context.Context) error { return nil }
