package account

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAccountUpsertDefaultsMatchPythonModel(t *testing.T) {
	upsert := NewAccountUpsert("tok")
	if upsert.Token != "tok" || upsert.Pool != "basic" {
		t.Fatalf("upsert token/pool = %#v", upsert)
	}
	if upsert.Tags == nil || len(upsert.Tags) != 0 {
		t.Fatalf("upsert tags = %#v", upsert.Tags)
	}
	if upsert.Ext == nil || len(upsert.Ext) != 0 {
		t.Fatalf("upsert ext = %#v", upsert.Ext)
	}

	raw, err := json.Marshal(upsert)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"token":"tok","pool":"basic","tags":[],"ext":{}}` {
		t.Fatalf("upsert json = %s", raw)
	}
}

func TestAccountPatchUsesPointerFieldsForPartialUpdates(t *testing.T) {
	pool := "super"
	status := AccountStatus("disabled")
	tags := []string{}
	addTags := []string{"a"}
	removeTags := []string{"b"}
	zero := 0
	lastClearAt := int64(123)
	reason := "manual"
	patch := AccountPatch{
		Token:         "tok",
		Pool:          &pool,
		Status:        &status,
		Tags:          tags,
		AddTags:       addTags,
		RemoveTags:    removeTags,
		QuotaGrok43:   map[string]any{"limit": 1},
		QuotaConsole:  map[string]any{"enabled": true},
		UsageUseDelta: &zero,
		LastClearAt:   &lastClearAt,
		StateReason:   &reason,
		ExtMerge:      map[string]any{"source": "test"},
		ClearFailures: true,
	}
	if patch.Pool == nil || *patch.Pool != "super" || patch.Status == nil || *patch.Status != "disabled" {
		t.Fatalf("patch optional fields = %#v", patch)
	}
	if patch.Tags == nil || len(patch.Tags) != 0 || !reflect.DeepEqual(patch.RemoveTags, removeTags) {
		t.Fatalf("patch list fields = %#v", patch)
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"token":"tok"`,
		`"pool":"super"`,
		`"status":"disabled"`,
		`"add_tags":["a"]`,
		`"remove_tags":["b"]`,
		`"quota_grok_4_3":{"limit":1}`,
		`"quota_console":{"enabled":true}`,
		`"usage_use_delta":0`,
		`"last_clear_at":123`,
		`"state_reason":"manual"`,
		`"ext_merge":{"source":"test"}`,
		`"clear_failures":true`,
	} {
		if !containsJSON(raw, want) {
			t.Fatalf("patch json %s missing %s", raw, want)
		}
	}
}

func TestListAccountsQueryDefaultsAndNormalize(t *testing.T) {
	query := DefaultListAccountsQuery()
	if query.Page != 1 || query.PageSize != 50 || query.SortBy != "updated_at" || !query.SortDesc {
		t.Fatalf("query defaults = %#v", query)
	}
	if query.Tags == nil || query.ExcludeTags == nil || query.IncludeDeleted {
		t.Fatalf("query slices/deleted = %#v", query)
	}
	query.Page = 0
	query.PageSize = 5000
	query.Tags = nil
	query.ExcludeTags = nil
	query.SortBy = ""
	query.Normalize()
	if query.Page != 1 || query.PageSize != 2000 || query.Tags == nil || query.ExcludeTags == nil || query.SortBy != "updated_at" {
		t.Fatalf("query normalized = %#v", query)
	}
	query.PageSize = 0
	query.Normalize()
	if query.PageSize != 50 {
		t.Fatalf("query page_size lower bound = %#v", query)
	}
	raw, err := json.Marshal(DefaultListAccountsQuery())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"page":1`,
		`"page_size":50`,
		`"tags":[]`,
		`"exclude_tags":[]`,
		`"include_deleted":false`,
		`"sort_by":"updated_at"`,
		`"sort_desc":true`,
	} {
		if !containsJSON(raw, want) {
			t.Fatalf("query json %s missing %s", raw, want)
		}
	}
}

func TestBulkReplacePoolCommandDefaults(t *testing.T) {
	cmd := BulkReplacePoolCommand{Pool: "super"}
	cmd.Normalize()
	if cmd.Pool != "super" || cmd.Upserts == nil {
		t.Fatalf("bulk command = %#v", cmd)
	}
	want := []AccountUpsert{{Token: "a", Pool: "basic", Tags: []string{}, Ext: map[string]any{}}}
	cmd.Upserts = []AccountUpsert{{Token: "a"}}
	cmd.Normalize()
	if !reflect.DeepEqual(cmd.Upserts, want) {
		t.Fatalf("bulk upserts = %#v", cmd.Upserts)
	}
}

func containsJSON(raw []byte, want string) bool {
	return strings.Contains(string(raw), want)
}
