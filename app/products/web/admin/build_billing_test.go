package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
)

type fakeBillingFetcher struct {
	calls   []string
	billing build.Billing
	err     error
	errOnce error
	n       int
}

func (f *fakeBillingFetcher) GetBilling(_ context.Context, accessToken string) (build.Billing, error) {
	f.calls = append(f.calls, accessToken)
	f.n++
	if f.errOnce != nil && f.n == 1 {
		return build.Billing{}, f.errOnce
	}
	if f.err != nil {
		return build.Billing{}, f.err
	}
	return f.billing, nil
}

func TestEnsureBuildAccessTokenRefusesDeadToken(t *testing.T) {
	store := &fakeBuildAccountStore{accounts: []buildaccount.Account{{
		ID:           1,
		AccessToken:  "dead-at",
		RefreshToken: "",
		ExpiresAt:    time.Now().UTC().Add(-time.Hour),
		Status:       buildaccount.StatusActive,
	}}}
	_, _, err := ensureBuildAccessToken(context.Background(), store, store.accounts[0])
	if err == nil {
		t.Fatal("expected error when access expired and no refresh")
	}
	if !strings.Contains(err.Error(), "expired") && !strings.Contains(err.Error(), "refresh") {
		t.Fatalf("err=%v", err)
	}
}

func TestFetchBuildBillingMapsUnauthorizedToValidation(t *testing.T) {
	store := &fakeBuildAccountStore{accounts: []buildaccount.Account{{
		ID:          2,
		AccessToken: "live-at",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
		Status:      buildaccount.StatusActive,
	}}}
	prev := adminBuildBillingFetcher
	adminBuildBillingFetcher = func() buildBillingFetcher {
		return &fakeBillingFetcher{err: &build.UpstreamError{Status: 401, Body: `{"reason":"no auth context"}`, Op: "get_billing"}}
	}
	defer func() { adminBuildBillingFetcher = prev }()

	_, err := fetchBuildBillingWithRefresh(context.Background(), store, store.accounts[0])
	if err == nil {
		t.Fatal("expected error")
	}
	mapped := mapBuildAdminError(err)
	rec := httptest.NewRecorder()
	writeAdminError(rec, mapped)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") && !strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestHandleAdminBuildAccountsBillingNoDeadTokenUpstream(t *testing.T) {
	store := &fakeBuildAccountStore{accounts: []buildaccount.Account{{
		ID:           3,
		AccessToken:  "dead-at",
		RefreshToken: "",
		ExpiresAt:    time.Now().UTC().Add(-2 * time.Hour),
		Status:       buildaccount.StatusActive,
	}}}
	restore := SetBuildAccountStore(store)
	defer restore()

	prev := adminBuildBillingFetcher
	fetcher := &fakeBillingFetcher{billing: build.Billing{MonthlyLimit: 1}}
	adminBuildBillingFetcher = func() buildBillingFetcher { return fetcher }
	defer func() { adminBuildBillingFetcher = prev }()

	body := []byte(`{"id":3}`)
	rec := httptest.NewRecorder()
	handleAdminBuildAccountsBilling(rec, httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/billing", strings.NewReader(string(body))))
	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("must not 500: body=%s", rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(fetcher.calls) != 0 {
		t.Fatalf("must not call GetBilling with dead token, calls=%v", fetcher.calls)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("payload=%v", payload)
	}
}

func TestMapBuildTokenErrorMarksExpired(t *testing.T) {
	store := &fakeBuildAccountStore{accounts: []buildaccount.Account{{
		ID:     9,
		Status: buildaccount.StatusActive,
	}}}
	err := mapBuildTokenError(context.Background(), store, 9, &build.RefreshError{
		Code:      "invalid_grant",
		Permanent: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if store.accounts[0].Status != buildaccount.StatusExpired {
		t.Fatalf("status=%s", store.accounts[0].Status)
	}
	rec := httptest.NewRecorder()
	writeAdminError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
