package backends

import (
	"context"
	"testing"

	account "github.com/dslzl/gork/app/control/account"
)

func TestSQLReplacePoolRollsBackWhenUpsertFails(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLRepository(t)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if _, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "basic-a", Pool: "basic"},
		{Token: "basic-b", Pool: "basic"},
	}); err != nil {
		t.Fatalf("seed UpsertAccounts returned error: %v", err)
	}

	_, err := repo.ReplacePool(ctx, account.BulkReplacePoolCommand{
		Pool: "basic",
		Upserts: []account.AccountUpsert{{
			Token: "basic-new",
			Pool:  "basic",
			Ext:   map[string]any{"bad": func() {}},
		}},
	})
	if err == nil {
		t.Fatal("ReplacePool succeeded with non-serializable ext")
	}

	assertLiveTokens(t, ctx, repo, map[string]bool{"basic-a": true, "basic-b": true})
}

func TestRedisReplacePoolRestoresOldPoolWhenUpsertFails(t *testing.T) {
	ctx := context.Background()
	repo := NewRedisAccountRepository(newFakeRedisAccountStore())
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if _, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "basic-a", Pool: "basic"},
		{Token: "basic-b", Pool: "basic"},
	}); err != nil {
		t.Fatalf("seed UpsertAccounts returned error: %v", err)
	}

	_, err := repo.ReplacePool(ctx, account.BulkReplacePoolCommand{
		Pool: "basic",
		Upserts: []account.AccountUpsert{{
			Token: "basic-new",
			Pool:  "basic",
			Ext:   map[string]any{"bad": func() {}},
		}},
	})
	if err == nil {
		t.Fatal("ReplacePool succeeded with non-serializable ext")
	}

	assertLiveTokens(t, ctx, repo, map[string]bool{"basic-a": true, "basic-b": true})
}

func TestRedisUpsertRestoresIndexesAfterMidWriteFailure(t *testing.T) {
	ctx := context.Background()
	store := newFakeRedisAccountStore()
	repo := NewRedisAccountRepository(store)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if _, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{{Token: "tok", Pool: "basic"}}); err != nil {
		t.Fatalf("seed UpsertAccounts returned error: %v", err)
	}
	store.failHSetKey = redisRecordKey("tok")

	_, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{{Token: "tok", Pool: "super"}})
	if err == nil {
		t.Fatal("UpsertAccounts succeeded despite forced HSet failure")
	}

	assertLiveTokens(t, ctx, repo, map[string]bool{"tok": true})
	records, err := repo.GetAccounts(ctx, []string{"tok"})
	if err != nil || len(records) != 1 {
		t.Fatalf("GetAccounts = %#v/%v, want tok", records, err)
	}
	if records[0].Pool != "basic" {
		t.Fatalf("record pool = %q, want restored basic", records[0].Pool)
	}
	page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{Page: 1, PageSize: 10, Pool: stringPtr("super")})
	if err != nil {
		t.Fatalf("ListAccounts(super) returned error: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("super index retained failed token: %#v", page.Items)
	}
}

func assertLiveTokens(t *testing.T, ctx context.Context, repo account.AccountRepository, want map[string]bool) {
	t.Helper()
	page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{Page: 1, PageSize: 50, SortBy: "token"})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	got := map[string]bool{}
	for _, item := range page.Items {
		got[item.Token] = true
	}
	if len(got) != len(want) {
		t.Fatalf("live tokens = %#v, want %#v", got, want)
	}
	for token := range want {
		if !got[token] {
			t.Fatalf("live tokens = %#v, missing %s", got, token)
		}
	}
}

func stringPtr(value string) *string {
	return &value
}
