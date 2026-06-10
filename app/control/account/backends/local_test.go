package backends

import (
	"context"
	"path/filepath"
	"testing"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

func TestLocalAccountRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "accounts.db")
	repo := NewLocalAccountRepository(dbPath)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	revision, err := repo.GetRevision(ctx)
	if err != nil || revision != 0 {
		t.Fatalf("initial revision = %d/%v, want 0/nil", revision, err)
	}

	upserted, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "sso= tok-a", Pool: "super", Tags: []string{"z", "b"}, Ext: map[string]any{"keep": "yes"}},
	})
	if err != nil || upserted.Upserted != 1 || upserted.Revision != 1 {
		t.Fatalf("UpsertAccounts = %#v/%v, want 1 rev 1", upserted, err)
	}

	failDelta, useDelta, syncDelta := 3, -5, 2
	lastFailAt, lastSyncAt := int64(111), int64(222)
	failReason := "bad"
	disabled := account.AccountStatusDisabled
	patched, err := repo.PatchAccounts(ctx, []account.AccountPatch{{
		Token:          "tok-a",
		Status:         &disabled,
		AddTags:        []string{"a"},
		RemoveTags:     []string{"b"},
		UsageUseDelta:  &useDelta,
		UsageFailDelta: &failDelta,
		UsageSyncDelta: &syncDelta,
		LastFailAt:     &lastFailAt,
		LastFailReason: &failReason,
		LastSyncAt:     &lastSyncAt,
		ExtMerge:       map[string]any{"disabled_at": float64(1), "merged": "ok"},
		ClearFailures:  true,
		QuotaConsole:   map[string]any{"remaining": float64(9), "total": float64(10)},
		QuotaGrok43:    map[string]any{"remaining": float64(4), "total": float64(5)},
		QuotaHeavy:     map[string]any{"remaining": float64(1), "total": float64(2)},
		QuotaAuto:      map[string]any{"remaining": float64(7), "total": float64(8)},
		QuotaFast:      map[string]any{"remaining": float64(6), "total": float64(7)},
		QuotaExpert:    map[string]any{"remaining": float64(5), "total": float64(6)},
	}})
	if err != nil || patched.Patched != 1 || patched.Revision != 2 {
		t.Fatalf("PatchAccounts = %#v/%v, want 1 rev 2", patched, err)
	}

	records, err := repo.GetAccounts(ctx, []string{"tok-a"})
	if err != nil || len(records) != 1 {
		t.Fatalf("GetAccounts = %#v/%v, want one record", records, err)
	}
	record := records[0]
	if record.Token != "tok-a" || record.Status != account.AccountStatusActive {
		t.Fatalf("patched record identity/status = %#v", record)
	}
	if record.UsageUseCount != 0 || record.UsageFailCount != 0 || record.UsageSyncCount != 2 {
		t.Fatalf("usage counters = %d/%d/%d", record.UsageUseCount, record.UsageFailCount, record.UsageSyncCount)
	}
	if record.LastFailAt != nil || record.LastFailReason != nil || record.StateReason != nil {
		t.Fatalf("failure state was not cleared: %#v", record)
	}
	if got := record.Tags; len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("tags = %#v, want [a z]", got)
	}
	if _, ok := record.Ext["disabled_at"]; ok || record.Ext["merged"] != "ok" || record.Ext["keep"] != "yes" {
		t.Fatalf("ext merge/clear = %#v", record.Ext)
	}

	changes, err := repo.ScanChanges(ctx, 1, 10)
	if err != nil || changes.Revision != 2 || len(changes.Items) != 1 || len(changes.DeletedTokens) != 0 {
		t.Fatalf("ScanChanges after patch = %#v/%v", changes, err)
	}
	deleted, err := repo.DeleteAccounts(ctx, []string{"tok-a"})
	if err != nil || deleted.Deleted != 1 || deleted.Revision != 3 {
		t.Fatalf("DeleteAccounts = %#v/%v, want 1 rev 3", deleted, err)
	}
	changes, err = repo.ScanChanges(ctx, 2, 10)
	if err != nil || len(changes.Items) != 0 || len(changes.DeletedTokens) != 1 || changes.DeletedTokens[0] != "tok-a" {
		t.Fatalf("ScanChanges after delete = %#v/%v", changes, err)
	}
}

func TestLocalAccountRepositoryListAndReplacePool(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "accounts.db")
	repository, err := CreateRepository(
		map[string]string{"ACCOUNT_STORAGE": "local", "ACCOUNT_LOCAL_PATH": dbPath},
		RepositoryConstructors{},
	)
	if err != nil {
		t.Fatalf("CreateRepository returned error: %v", err)
	}
	repo, ok := repository.(*LocalAccountRepository)
	if !ok {
		t.Fatalf("CreateRepository returned %T, want *LocalAccountRepository", repository)
	}
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	_, err = repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "basic-a", Pool: "basic", Tags: []string{"x"}},
		{Token: "basic-b", Pool: "basic", Tags: []string{"x", "drop"}},
		{Token: "super-a", Pool: "super", Tags: []string{"y"}},
	})
	if err != nil {
		t.Fatalf("UpsertAccounts returned error: %v", err)
	}

	pool := "basic"
	page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{
		Page:        1,
		PageSize:    10,
		Pool:        &pool,
		Tags:        []string{"x"},
		ExcludeTags: []string{"drop"},
		SortBy:      "token",
	})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Token != "basic-a" {
		t.Fatalf("ListAccounts filtered page = %#v/%v", page, err)
	}
	facets, err := repo.ListFacets(ctx)
	if err != nil {
		t.Fatalf("ListFacets returned error: %v", err)
	}
	if facets.Pools["basic"] != 2 || facets.Pools["super"] != 1 || facets.Status["active"] != 3 || facets.NSFW["disabled"] != 3 {
		t.Fatalf("ListFacets counts = %#v", facets)
	}

	replaced, err := repo.ReplacePool(ctx, account.BulkReplacePoolCommand{
		Pool: "basic",
		Upserts: []account.AccountUpsert{
			{Token: "basic-new", Pool: "basic", Tags: []string{"x"}},
		},
	})
	if err != nil || replaced.Upserted != 1 || replaced.Deleted != 2 || replaced.Revision != 3 {
		t.Fatalf("ReplacePool = %#v/%v, want 1 upsert 2 deleted rev 3", replaced, err)
	}
	snapshot, err := repo.RuntimeSnapshot(ctx)
	if err != nil {
		t.Fatalf("RuntimeSnapshot returned error: %v", err)
	}
	tokens := map[string]bool{}
	for _, item := range snapshot.Items {
		tokens[item.Token] = true
	}
	if len(tokens) != 2 || !tokens["basic-new"] || !tokens["super-a"] {
		t.Fatalf("snapshot tokens after replace = %#v", tokens)
	}
}
