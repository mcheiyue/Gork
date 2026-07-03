package backends

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	account "github.com/dslzl/gork/app/control/account"
)

func TestRedisAccountRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newFakeRedisAccountStore()
	repo := NewRedisAccountRepository(store)
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
		QuotaAuto:      map[string]any{"remaining": float64(7), "total": float64(8)},
		QuotaFast:      map[string]any{"remaining": float64(6), "total": float64(7)},
		QuotaExpert:    map[string]any{"remaining": float64(5), "total": float64(6)},
		QuotaHeavy:     map[string]any{"remaining": float64(1), "total": float64(2)},
		QuotaGrok43:    map[string]any{"remaining": float64(4), "total": float64(5)},
		QuotaConsole:   map[string]any{"remaining": float64(3), "total": float64(4)},
	}})
	if err != nil || patched.Patched != 1 || patched.Revision != 2 {
		t.Fatalf("PatchAccounts = %#v/%v, want 1 rev 2", patched, err)
	}

	records, err := repo.GetAccounts(ctx, []string{"tok-a"})
	if err != nil || len(records) != 1 {
		t.Fatalf("GetAccounts = %#v/%v, want one record", records, err)
	}
	record := records[0]
	if record.Status != account.AccountStatusActive {
		t.Fatalf("clear_failures status = %q, want active", record.Status)
	}
	if record.UsageUseCount != 0 || record.UsageFailCount != 0 || record.UsageSyncCount != 2 {
		t.Fatalf("usage counters = %d/%d/%d", record.UsageUseCount, record.UsageFailCount, record.UsageSyncCount)
	}
	if got := record.Tags; len(got) != 2 || got[0] != "z" || got[1] != "a" {
		t.Fatalf("redis patch tags = %#v, want [z a]", got)
	}
	if _, ok := record.Ext["disabled_at"]; ok || record.Ext["merged"] != "ok" || record.Ext["keep"] != "yes" {
		t.Fatalf("ext merge/clear = %#v", record.Ext)
	}
	quota, err := record.QuotaSet()
	if err != nil {
		t.Fatalf("QuotaSet returned error: %v", err)
	}
	if quota.Grok43 == nil || quota.Grok43.Remaining != 4 || quota.Grok43.Total != 5 {
		t.Fatalf("quota_grok_4_3 = %#v, want remaining 4 total 5", quota.Grok43)
	}
	if quota.Console == nil || quota.Console.Remaining != 3 || quota.Console.Total != 4 {
		t.Fatalf("quota_console = %#v, want remaining 3 total 4", quota.Console)
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

func TestRedisAccountRepositoryIndexedListAndReplacePool(t *testing.T) {
	ctx := context.Background()
	store := newFakeRedisAccountStore()
	repo := NewRedisAccountRepository(store)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	_, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
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
	tokens := []string{}
	for _, item := range snapshot.Items {
		tokens = append(tokens, item.Token)
	}
	sort.Strings(tokens)
	if strings.Join(tokens, ",") != "basic-new,super-a" {
		t.Fatalf("snapshot tokens after replace = %#v", tokens)
	}
}

func TestRedisAccountRepositoryUsesIndexesWithoutScanningRecords(t *testing.T) {
	ctx := context.Background()
	store := newFakeRedisAccountStore()
	repo := NewRedisAccountRepository(store)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	_, err := repo.UpsertAccounts(ctx, []account.AccountUpsert{
		{Token: "tok-001", Pool: "basic", Tags: []string{"nsfw"}},
		{Token: "tok-002", Pool: "basic"},
		{Token: "tok-003", Pool: "super", Tags: []string{"nsfw"}},
	})
	if err != nil {
		t.Fatalf("UpsertAccounts returned error: %v", err)
	}
	disabled := account.AccountStatusDisabled
	if _, err := repo.PatchAccounts(ctx, []account.AccountPatch{{Token: "tok-002", Status: &disabled}}); err != nil {
		t.Fatalf("PatchAccounts returned error: %v", err)
	}

	store.failScan = true
	page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{
		Tags:     []string{"nsfw"},
		Page:     1,
		PageSize: 10,
		SortBy:   "token",
	})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	got := []string{}
	for _, item := range page.Items {
		got = append(got, item.Token)
	}
	if page.Total != 2 || strings.Join(got, ",") != "tok-001,tok-003" {
		t.Fatalf("indexed tag list total=%d tokens=%v", page.Total, got)
	}
}

func TestRedisAccountRepositoryInitializeRebuildsMissingIndexesForExistingRecords(t *testing.T) {
	ctx := context.Background()
	store := newFakeRedisAccountStore()
	store.strings[redisKeyRevision] = "1"
	repo := NewRedisAccountRepository(store)
	record, err := account.NewAccountRecord(account.AccountRecord{Token: "tok-old", Pool: "basic", Tags: []string{"legacy"}})
	if err != nil {
		t.Fatalf("NewAccountRecord returned error: %v", err)
	}
	record.CreatedAt = 1
	record.UpdatedAt = 1
	hash, err := redisHashFromRecord(record, 1)
	if err != nil {
		t.Fatalf("redisHashFromRecord returned error: %v", err)
	}
	store.hashes[redisRecordKey("tok-old")] = hash

	if err := repo.Initialize(ctx); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	store.failScan = true
	page, err := repo.ListAccounts(ctx, account.ListAccountsQuery{
		Tags:     []string{"legacy"},
		Page:     1,
		PageSize: 10,
		SortBy:   "token",
	})
	if err != nil {
		t.Fatalf("ListAccounts returned error: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Token != "tok-old" {
		t.Fatalf("rebuilt index page = %#v", page)
	}
}

type fakeRedisAccountStore struct {
	strings     map[string]string
	hashes      map[string]map[string]string
	sets        map[string]map[string]struct{}
	zsets       map[string]map[string]int
	closed      bool
	failScan    bool
	failHSetKey string
}

var errFakeRedisScanForbidden = errors.New("scan should not be used")

func newFakeRedisAccountStore() *fakeRedisAccountStore {
	return &fakeRedisAccountStore{
		strings: map[string]string{},
		hashes:  map[string]map[string]string{},
		sets:    map[string]map[string]struct{}{},
		zsets:   map[string]map[string]int{},
	}
}

func (s *fakeRedisAccountStore) Incr(_ context.Context, key string) (int, error) {
	current := 0
	if raw, ok := s.strings[key]; ok {
		current = atoiForFake(raw)
	}
	current++
	s.strings[key] = itoaForFake(current)
	return current, nil
}

func (s *fakeRedisAccountStore) SetNX(_ context.Context, key string, value string) (bool, error) {
	if _, ok := s.strings[key]; ok {
		return false, nil
	}
	s.strings[key] = value
	return true, nil
}

func (s *fakeRedisAccountStore) Get(_ context.Context, key string) (string, bool, error) {
	value, ok := s.strings[key]
	return value, ok, nil
}

func (s *fakeRedisAccountStore) ScanKeys(_ context.Context, pattern string) ([]string, error) {
	if s.failScan {
		return nil, errFakeRedisScanForbidden
	}
	prefix := strings.TrimSuffix(pattern, "*")
	keys := []string{}
	for key := range s.hashes {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *fakeRedisAccountStore) Del(_ context.Context, key string) error {
	delete(s.hashes, key)
	return nil
}

func (s *fakeRedisAccountStore) HGetAll(_ context.Context, key string) (map[string]string, error) {
	src := s.hashes[key]
	dst := map[string]string{}
	for k, v := range src {
		dst[k] = v
	}
	return dst, nil
}

func (s *fakeRedisAccountStore) HGet(_ context.Context, key string, field string) (string, bool, error) {
	hash := s.hashes[key]
	value, ok := hash[field]
	return value, ok, nil
}

func (s *fakeRedisAccountStore) HSet(_ context.Context, key string, mapping map[string]string) error {
	if s.failHSetKey == key {
		s.failHSetKey = ""
		return errors.New("forced hset failure")
	}
	if s.hashes[key] == nil {
		s.hashes[key] = map[string]string{}
	}
	for field, value := range mapping {
		s.hashes[key][field] = value
	}
	return nil
}

func (s *fakeRedisAccountStore) ZAdd(_ context.Context, key string, members map[string]int) error {
	if s.zsets[key] == nil {
		s.zsets[key] = map[string]int{}
	}
	for member, score := range members {
		s.zsets[key][member] = score
	}
	return nil
}

func (s *fakeRedisAccountStore) ZRangeByScore(_ context.Context, key string, minExclusive int, limit int) ([]string, error) {
	type item struct {
		member string
		score  int
	}
	items := []item{}
	for member, score := range s.zsets[key] {
		if score > minExclusive {
			items = append(items, item{member, score})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score < items[j].score })
	out := []string{}
	for _, item := range items {
		out = append(out, item.member)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *fakeRedisAccountStore) ZRem(_ context.Context, key string, members ...string) error {
	for _, member := range members {
		delete(s.zsets[key], member)
	}
	return nil
}

func (s *fakeRedisAccountStore) SAdd(_ context.Context, key string, members ...string) error {
	if s.sets[key] == nil {
		s.sets[key] = map[string]struct{}{}
	}
	for _, member := range members {
		s.sets[key][member] = struct{}{}
	}
	return nil
}

func (s *fakeRedisAccountStore) SRem(_ context.Context, key string, members ...string) error {
	for _, member := range members {
		delete(s.sets[key], member)
	}
	return nil
}

func (s *fakeRedisAccountStore) SMembers(_ context.Context, key string) ([]string, error) {
	members := []string{}
	for member := range s.sets[key] {
		members = append(members, member)
	}
	sort.Strings(members)
	return members, nil
}

func (s *fakeRedisAccountStore) Close(context.Context) error {
	s.closed = true
	return nil
}

func atoiForFake(raw string) int {
	out := 0
	for _, ch := range raw {
		out = out*10 + int(ch-'0')
	}
	return out
}

func itoaForFake(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
