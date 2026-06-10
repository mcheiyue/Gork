package account

import (
	"context"
	"testing"
)

type fakeAccountRepository struct{}

type observedAccountRepository struct {
	fakeAccountRepository
	scanSince      int
	scanLimit      int
	upsertTokens   []string
	patchTokens    []string
	deleteTokens   []string
	getTokens      []string
	listQuery      ListAccountsQuery
	replacePool    string
	replaceUpserts int
	closed         bool
	initialized    bool
}

var _ AccountRepository = (*observedAccountRepository)(nil)

func (r *observedAccountRepository) Initialize(context.Context) error {
	r.initialized = true
	return nil
}

func (fakeAccountRepository) GetRevision(context.Context) (int, error) {
	return 7, nil
}

func (fakeAccountRepository) RuntimeSnapshot(context.Context) (RuntimeSnapshot, error) {
	return RuntimeSnapshot{Items: []AccountRecord{{Token: "snapshot-token"}}, Revision: 6}, nil
}

func (r *observedAccountRepository) ScanChanges(_ context.Context, sinceRevision int, limit int) (AccountChangeSet, error) {
	r.scanSince = sinceRevision
	r.scanLimit = limit
	return AccountChangeSet{Items: []AccountRecord{{Token: "changed-token"}}, Revision: 8}, nil
}

func (r *observedAccountRepository) UpsertAccounts(_ context.Context, items []AccountUpsert) (AccountMutationResult, error) {
	for _, item := range items {
		r.upsertTokens = append(r.upsertTokens, item.Token)
	}
	return AccountMutationResult{Upserted: 1, Revision: 2}, nil
}

func (r *observedAccountRepository) PatchAccounts(_ context.Context, patches []AccountPatch) (AccountMutationResult, error) {
	for _, patch := range patches {
		r.patchTokens = append(r.patchTokens, patch.Token)
	}
	return AccountMutationResult{Patched: 1, Revision: 3}, nil
}

func (r *observedAccountRepository) DeleteAccounts(_ context.Context, tokens []string) (AccountMutationResult, error) {
	r.deleteTokens = append([]string(nil), tokens...)
	return AccountMutationResult{Deleted: 1, Revision: 4}, nil
}

func (r *observedAccountRepository) GetAccounts(_ context.Context, tokens []string) ([]AccountRecord, error) {
	r.getTokens = append([]string(nil), tokens...)
	return []AccountRecord{{Token: tokens[0]}}, nil
}

func (r *observedAccountRepository) ListAccounts(_ context.Context, query ListAccountsQuery) (AccountPage, error) {
	r.listQuery = query
	page := NewAccountPage()
	page.Items = []AccountRecord{{Token: "listed-token"}}
	return page, nil
}

func (r *observedAccountRepository) ReplacePool(_ context.Context, command BulkReplacePoolCommand) (AccountMutationResult, error) {
	r.replacePool = command.Pool
	r.replaceUpserts = len(command.Upserts)
	return AccountMutationResult{Revision: 5}, nil
}

func (r *observedAccountRepository) Close(context.Context) error {
	r.closed = true
	return nil
}

func TestAccountRepositoryContract(t *testing.T) {
	observed := &observedAccountRepository{}
	repo := AccountRepository(observed)
	ctx := context.Background()

	if err := repo.Initialize(ctx); err != nil || !observed.initialized {
		t.Fatalf("Initialize = %v, initialized=%v", err, observed.initialized)
	}
	revision, err := repo.GetRevision(ctx)
	if err != nil || revision != 7 {
		t.Fatalf("GetRevision = %d/%v, want 7/nil", revision, err)
	}
	snapshot, err := repo.RuntimeSnapshot(ctx)
	if err != nil || snapshot.Revision != 6 || len(snapshot.Items) != 1 {
		t.Fatalf("RuntimeSnapshot = %#v/%v", snapshot, err)
	}
	changes, err := repo.ScanChanges(ctx, 10, AccountScanChangesDefaultLimit)
	if err != nil {
		t.Fatalf("ScanChanges returned error: %v", err)
	}
	if changes.Revision != 8 || observed.scanSince != 10 || observed.scanLimit != 5000 || AccountScanChangesDefaultLimit != 5000 {
		t.Fatalf("scan changes/default limit = %#v/%d/%d/%d", changes, observed.scanSince, observed.scanLimit, AccountScanChangesDefaultLimit)
	}
	upserted, err := repo.UpsertAccounts(ctx, []AccountUpsert{{Token: "upsert-token"}})
	if err != nil || upserted.Upserted != 1 || observed.upsertTokens[0] != "upsert-token" {
		t.Fatalf("UpsertAccounts = %#v/%v tokens=%#v", upserted, err, observed.upsertTokens)
	}
	mutation, err := repo.PatchAccounts(ctx, []AccountPatch{{Token: "tok"}})
	if err != nil || mutation.Patched != 1 || observed.patchTokens[0] != "tok" {
		t.Fatalf("PatchAccounts = %#v/%v tokens=%#v", mutation, err, observed.patchTokens)
	}
	deleted, err := repo.DeleteAccounts(ctx, []string{"delete-token"})
	if err != nil || deleted.Deleted != 1 || observed.deleteTokens[0] != "delete-token" {
		t.Fatalf("DeleteAccounts = %#v/%v tokens=%#v", deleted, err, observed.deleteTokens)
	}
	records, err := repo.GetAccounts(ctx, []string{"get-token"})
	if err != nil || len(records) != 1 || records[0].Token != "get-token" || observed.getTokens[0] != "get-token" {
		t.Fatalf("GetAccounts = %#v/%v tokens=%#v", records, err, observed.getTokens)
	}
	page, err := repo.ListAccounts(ctx, ListAccountsQuery{Page: 1})
	if err != nil || page.Page != 1 || page.PageSize != 50 {
		t.Fatalf("ListAccounts defaults = %#v/%v", page, err)
	}
	if observed.listQuery.Page != 1 {
		t.Fatalf("ListAccounts query = %#v", observed.listQuery)
	}
	replaced, err := repo.ReplacePool(ctx, BulkReplacePoolCommand{Pool: "basic", Upserts: []AccountUpsert{{Token: "replace-token"}}})
	if err != nil || replaced.Revision != 5 || observed.replacePool != "basic" || observed.replaceUpserts != 1 {
		t.Fatalf("ReplacePool = %#v/%v pool=%s upserts=%d", replaced, err, observed.replacePool, observed.replaceUpserts)
	}
	if err := repo.Close(ctx); err != nil || !observed.closed {
		t.Fatalf("Close = %v closed=%v", err, observed.closed)
	}
}
