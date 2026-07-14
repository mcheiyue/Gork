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
	"github.com/dslzl/gork/app/dataplane/build"
)

type stubDeviceOAuth struct {
	pending bool
	token   build.TokenPayload
}

func (s stubDeviceOAuth) StartDevice(context.Context) (build.DeviceAuthorization, error) {
	return build.DeviceAuthorization{
		DeviceCode:      "dc",
		UserCode:        "UC-1",
		VerificationURI: "https://example/verify",
		Interval:        5 * time.Second,
		ExpiresIn:       1800 * time.Second,
	}, nil
}

func (s stubDeviceOAuth) PollDevice(context.Context, string) (build.TokenPayload, error) {
	if s.pending {
		return build.TokenPayload{}, build.ErrAuthorizationPending
	}
	return s.token, nil
}

type stubAdminBuildStore struct {
	last buildaccount.Account
}

func (s *stubAdminBuildStore) List(context.Context) ([]buildaccount.Account, error) {
	return nil, nil
}
func (s *stubAdminBuildStore) Upsert(_ context.Context, account buildaccount.Account) (buildaccount.Account, error) {
	account.ID = 9
	s.last = account
	return account, nil
}
func (s *stubAdminBuildStore) Delete(context.Context, int64) error { return nil }
func (s *stubAdminBuildStore) SetStatus(context.Context, int64, string, string) error {
	return nil
}

func TestHandleAdminBuildDeviceStart(t *testing.T) {
	prev := adminBuildOAuth
	adminBuildOAuth = func() buildDeviceOAuth { return stubDeviceOAuth{} }
	defer func() { adminBuildOAuth = prev }()

	req := httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/device/start", nil)
	rr := httptest.NewRecorder()
	handleAdminBuildDeviceStart(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["user_code"] != "UC-1" || body["device_code"] != "dc" {
		t.Fatalf("%#v", body)
	}
}

func TestHandleAdminBuildDevicePollPendingAndSuccess(t *testing.T) {
	store := &stubAdminBuildStore{}
	restore := SetBuildAccountStore(store)
	defer restore()

	prev := adminBuildOAuth
	adminBuildOAuth = func() buildDeviceOAuth {
		return stubDeviceOAuth{pending: true}
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/device/poll",
		bytes.NewReader([]byte(`{"device_code":"dc"}`)))
	rr := httptest.NewRecorder()
	handleAdminBuildDevicePoll(rr, req)
	if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(`"pending"`)) {
		t.Fatalf("pending: %s", rr.Body.String())
	}

	adminBuildOAuth = func() buildDeviceOAuth {
		return stubDeviceOAuth{token: build.TokenPayload{
			AccessToken:  "at",
			RefreshToken: "rt",
			ExpiresAt:    time.Now().UTC().Add(time.Hour),
		}}
	}
	defer func() { adminBuildOAuth = prev }()

	req2 := httptest.NewRequest(http.MethodPost, "/admin/api/build-accounts/device/poll",
		bytes.NewReader([]byte(`{"device_code":"dc","name":"n1"}`)))
	rr2 := httptest.NewRecorder()
	handleAdminBuildDevicePoll(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("success code=%d body=%s", rr2.Code, rr2.Body.String())
	}
	if store.last.AccessToken != "at" {
		t.Fatalf("%#v", store.last)
	}
	if bytes.Contains(rr2.Body.Bytes(), []byte("at")) {
		// serialize must not echo raw token
		// has_access_token ok, raw token not
		var body map[string]any
		_ = json.Unmarshal(rr2.Body.Bytes(), &body)
		acc, _ := body["account"].(map[string]any)
		if acc["access_token"] != nil {
			t.Fatalf("token leaked: %#v", acc)
		}
	}
}
