package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
)


type fakeBuildAccountStore struct {
	accounts []buildaccount.Account
	nextID   int64
}

func (f *fakeBuildAccountStore) List(context.Context) ([]buildaccount.Account, error) {
	out := make([]buildaccount.Account, len(f.accounts))
	copy(out, f.accounts)
	return out, nil
}

func (f *fakeBuildAccountStore) Upsert(_ context.Context, account buildaccount.Account) (buildaccount.Account, error) {
	if account.ID == 0 {
		f.nextID++
		account.ID = f.nextID
	}
	for i, existing := range f.accounts {
		if existing.UserID != "" && existing.UserID == account.UserID {
			account.ID = existing.ID
			f.accounts[i] = account
			return account, nil
		}
	}
	f.accounts = append(f.accounts, account)
	return account, nil
}

func (f *fakeBuildAccountStore) Delete(_ context.Context, id int64) error {
	filtered := f.accounts[:0]
	for _, acc := range f.accounts {
		if acc.ID != id {
			filtered = append(filtered, acc)
		}
	}
	f.accounts = filtered
	return nil
}

func (f *fakeBuildAccountStore) SetStatus(_ context.Context, id int64, status string, _ string) error {
	for i, acc := range f.accounts {
		if acc.ID == id {
			acc.Status = status
			f.accounts[i] = acc
			return nil
		}
	}
	return nil
}

func (f *fakeBuildAccountStore) Get(_ context.Context, id int64) (buildaccount.Account, error) {
	for _, acc := range f.accounts {
		if acc.ID == id {
			return acc, nil
		}
	}
	return buildaccount.Account{}, fmt.Errorf("build account not found")
}

func (f *fakeBuildAccountStore) UpdateBilling(_ context.Context, id int64, billing build.Billing) error {
	for i, acc := range f.accounts {
		if acc.ID == id {
			acc.Billing = billing
			acc.BillingSynced = billing.SyncedAt
			f.accounts[i] = acc
			return nil
		}
	}
	return nil
}

func (f *fakeBuildAccountStore) UpdateTokens(_ context.Context, id int64, access, refresh string, expiresAt time.Time) error {
	for i, acc := range f.accounts {
		if acc.ID == id {
			acc.AccessToken = access
			acc.RefreshToken = refresh
			acc.ExpiresAt = expiresAt
			f.accounts[i] = acc
			return nil
		}
	}
	return nil
}

func TestAdminBuildAccountsListImportStatusDelete(t *testing.T) {
	store := &fakeBuildAccountStore{}
	restore := SetBuildAccountStore(store)
	defer restore()

	importBody := []byte(`{"accounts":[{"provider":"grok_build","name":"n1","user_id":"u1","access_token":"at","refresh_token":"rt"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/import", bytes.NewReader(importBody))
	rec := httptest.NewRecorder()
	handleAdminBuildAccountsImport(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}
	var imported map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported["imported"] != float64(1) {
		t.Fatalf("imported=%v", imported["imported"])
	}
	if store.accounts[0].AccessToken != "at" {
		t.Fatalf("store token lost")
	}

	rec = httptest.NewRecorder()
	handleAdminBuildAccountsList(rec, httptest.NewRequest(http.MethodGet, "/admin/api/build-accounts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var listed struct {
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Accounts) != 1 {
		t.Fatalf("accounts=%d", len(listed.Accounts))
	}
	item := listed.Accounts[0]
	if _, ok := item["access_token"]; ok {
		t.Fatalf("access_token field present")
	}
	if _, ok := item["refresh_token"]; ok {
		t.Fatalf("refresh_token field present")
	}
	if item["has_access_token"] != true || item["has_refresh_token"] != true {
		t.Fatalf("token flags=%#v", item)
	}

	statusBody := []byte(`{"id":1,"status":"disabled","reason":"test"}`)
	rec = httptest.NewRecorder()
	handleAdminBuildAccountsStatus(rec, httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/status", bytes.NewReader(statusBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if store.accounts[0].Status != buildaccount.StatusDisabled {
		t.Fatalf("status=%s", store.accounts[0].Status)
	}

	rec = httptest.NewRecorder()
	handleAdminBuildAccountsDelete(rec, httptest.NewRequest(http.MethodDelete, "/admin/api/build-accounts?id=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}
	if len(store.accounts) != 0 {
		t.Fatalf("accounts left=%d", len(store.accounts))
	}
}

func TestAdminBuildAccountsStoreMissing(t *testing.T) {
	restore := SetBuildAccountStore(nil)
	defer restore()
	rec := httptest.NewRecorder()
	handleAdminBuildAccountsList(rec, httptest.NewRequest(http.MethodGet, "/admin/api/build-accounts", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSerializeBuildAccountHidesTokens(t *testing.T) {
	item := serializeBuildAccount(buildaccount.Account{
		ID: 9, Name: "n", AccessToken: "secret", RefreshToken: "secret2",
		Status: buildaccount.StatusActive, ExpiresAt: time.Unix(1700000000, 0).UTC(),
	})
	raw, _ := json.Marshal(item)
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatalf("token leaked: %s", raw)
	}
	if item["has_access_token"] != true || item["has_refresh_token"] != true {
		t.Fatalf("flags=%#v", item)
	}
}

func TestAdminBuildAccountsListFilter(t *testing.T) {
	store := &fakeBuildAccountStore{accounts: []buildaccount.Account{
		{ID: 1, Name: "ok", UserID: "u1", Status: buildaccount.StatusActive, AccessToken: "a", ExpiresAt: time.Now().UTC().Add(time.Hour)},
		{ID: 2, Name: "old", UserID: "u2", Status: buildaccount.StatusDisabled, AccessToken: "b", ExpiresAt: time.Now().UTC().Add(-time.Hour)},
	}}
	restore := SetBuildAccountStore(store)
	defer restore()

	rec := httptest.NewRecorder()
	handleAdminBuildAccountsList(rec, httptest.NewRequest(http.MethodGet, "/admin/api/build-accounts?status=disabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Accounts []map[string]any `json:"accounts"`
		Total    int              `json:"total"`
		Facets   map[string]any   `json:"facets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || listed.Accounts[0]["name"] != "old" {
		t.Fatalf("listed=%#v", listed)
	}
	if listed.Facets["all"] != float64(2) {
		t.Fatalf("facets=%#v", listed.Facets)
	}

	rec = httptest.NewRecorder()
	handleAdminBuildAccountsList(rec, httptest.NewRequest(http.MethodGet, "/admin/api/build-accounts?expired=true", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if listed.Total != 1 || listed.Accounts[0]["name"] != "old" {
		t.Fatalf("expired filter=%#v", listed)
	}
}

func TestAdminBuildAccountsImportAsyncAndCleanup(t *testing.T) {
	store := &fakeBuildAccountStore{}
	restore := SetBuildAccountStore(store)
	defer restore()
	// 同步 runner 便于测试
	prev := adminBuildAsyncRunner
	adminBuildAsyncRunner = func(run func()) { run() }
	defer func() { adminBuildAsyncRunner = prev }()

	body := []byte(`{"accounts":[
		{"provider":"grok_build","name":"a1","user_id":"ua","access_token":"at1","refresh_token":"rt1","expires_at":"2000-01-01T00:00:00Z"},
		{"provider":"grok_build","name":"a2","user_id":"ub","access_token":"at2","refresh_token":"rt2","expires_at":"2099-01-01T00:00:00Z"}
	]}`)
	rec := httptest.NewRecorder()
	handleAdminBuildAccountsImportAsync(rec, httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/import-async", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("import-async status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["task_id"] == "" || resp["total"] != float64(2) {
		t.Fatalf("resp=%#v", resp)
	}
	if len(store.accounts) != 2 {
		t.Fatalf("accounts=%d", len(store.accounts))
	}

	rec = httptest.NewRecorder()
	handleAdminBuildAccountsCleanup(rec, httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/cleanup", bytes.NewReader([]byte(`{"mode":"expired"}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("cleanup status=%d body=%s", rec.Code, rec.Body.String())
	}
	// 同步 runner 下 cleanup 已完成
	if len(store.accounts) != 1 || store.accounts[0].Name != "a2" {
		t.Fatalf("after cleanup=%#v", store.accounts)
	}
}
