package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
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
