package backends

import (
	"context"
	"path/filepath"
	"testing"

	account "github.com/dslzl/gork/app/control/account"
)

func TestAccountRepositoriesScanWholeRevisionBeyondLimit(t *testing.T) {
	tests := []struct {
		name string
		repo func(t *testing.T) account.AccountRepository
	}{
		{
			name: "local",
			repo: func(t *testing.T) account.AccountRepository {
				t.Helper()
				return NewLocalAccountRepository(filepath.Join(t.TempDir(), "accounts.db"))
			},
		},
		{
			name: "sql",
			repo: func(t *testing.T) account.AccountRepository {
				t.Helper()
				return newTestSQLRepository(t)
			},
		},
		{
			name: "redis",
			repo: func(t *testing.T) account.AccountRepository {
				t.Helper()
				return NewRedisAccountRepository(newFakeRedisAccountStore())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := tc.repo(t)
			if err := repo.Initialize(ctx); err != nil {
				t.Fatalf("Initialize returned error: %v", err)
			}
			t.Cleanup(func() { _ = repo.Close(context.Background()) })

			upserted, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
				{Token: "tok-a", Pool: "basic"},
				{Token: "tok-b", Pool: "basic"},
				{Token: "tok-c", Pool: "basic"},
			})
			if err != nil || upserted.Revision != 1 {
				t.Fatalf("UpsertAccounts = %#v/%v, want revision 1", upserted, err)
			}
			secondUpsert, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
				{Token: "tok-d", Pool: "basic"},
			})
			if err != nil || secondUpsert.Revision != 2 {
				t.Fatalf("second UpsertAccounts = %#v/%v, want revision 2", secondUpsert, err)
			}

			first, err := repo.ScanChanges(ctx, 0, 1)
			if err != nil {
				t.Fatalf("ScanChanges first returned error: %v", err)
			}
			if first.Revision != 1 || !first.HasMore || len(first.Items) != 3 || len(first.DeletedTokens) != 0 {
				t.Fatalf("first ScanChanges = %#v, want all three revision-1 items with HasMore", first)
			}
			second, err := repo.ScanChanges(ctx, first.Revision, 1)
			if err != nil {
				t.Fatalf("ScanChanges second returned error: %v", err)
			}
			if second.Revision != 2 || second.HasMore || len(second.Items) != 1 || second.Items[0].Token != "tok-d" {
				t.Fatalf("second ScanChanges = %#v, want tok-d revision 2 without HasMore", second)
			}
		})
	}
}
