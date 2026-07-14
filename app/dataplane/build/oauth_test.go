package build

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestStartDeviceSuccess(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := req.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if req.FormValue("client_id") != DefaultOAuthClientID {
			t.Fatalf("client_id=%q", req.FormValue("client_id"))
		}
		if !strings.Contains(req.FormValue("scope"), "offline_access") {
			t.Fatalf("scope=%q", req.FormValue("scope"))
		}
		body := `{"device_code":"dc","user_code":"UC-1","verification_uri":"https://auth.x.ai/device","verification_uri_complete":"https://auth.x.ai/device?user_code=UC-1","interval":5,"expires_in":600}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
	client := NewOAuthClient(httpClient, OAuthConfig{})
	auth, err := client.StartDevice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if auth.DeviceCode != "dc" || auth.UserCode != "UC-1" || auth.Interval != 5*time.Second {
		t.Fatalf("auth=%#v", auth)
	}
}

func TestPollDeviceClassifiesPendingAndSlowDown(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "pending", body: `{"error":"authorization_pending"}`, want: ErrAuthorizationPending},
		{name: "slow_down", body: `{"error":"slow_down"}`, want: ErrSlowDown},
		{name: "denied", body: `{"error":"access_denied"}`, want: ErrAuthorizationDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.FormValue("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
					t.Fatalf("grant_type=%q", req.FormValue("grant_type"))
				}
				return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header), Request: req}, nil
			})}
			_, err := NewOAuthClient(httpClient, OAuthConfig{}).PollDevice(context.Background(), "dc")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestRefreshClassifiesPermanentAndTransient(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		permanent  bool
		code       string
	}{
		{name: "transient", status: http.StatusServiceUnavailable, body: `{"error":"temporarily_unavailable"}`, retryAfter: "7", permanent: false, code: "temporarily_unavailable"},
		{name: "invalid_grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`, permanent: true, code: "invalid_grant"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.FormValue("grant_type") != "refresh_token" || req.FormValue("refresh_token") != "refresh" {
					t.Fatalf("form=%v", req.Form)
				}
				header := make(http.Header)
				if tc.retryAfter != "" {
					header.Set("Retry-After", tc.retryAfter)
				}
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})}
			_, err := NewOAuthClient(httpClient, OAuthConfig{}).Refresh(context.Background(), "refresh")
			var re *RefreshError
			if !errors.As(err, &re) || re.Permanent != tc.permanent || re.Code != tc.code {
				t.Fatalf("err=%#v", err)
			}
			if tc.retryAfter != "" && re.RetryAfter != 7*time.Second {
				t.Fatalf("retryAfter=%s", re.RetryAfter)
			}
			if IsPermanentRefresh(err) != tc.permanent {
				t.Fatalf("IsPermanentRefresh mismatch")
			}
		})
	}
}

func TestRefreshSuccessKeepsFallbackRefreshToken(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"access_token":"at","expires_in":120,"token_type":"Bearer"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
	payload, err := NewOAuthClient(httpClient, OAuthConfig{}).Refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if payload.AccessToken != "at" || payload.RefreshToken != "old-refresh" {
		t.Fatalf("payload=%#v", payload)
	}
	if payload.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("expires_at=%s", payload.ExpiresAt)
	}
}

func TestPollDeviceSuccess(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"access_token":"at","refresh_token":"rt","expires_in":3600,"id_token":"hdr.e30.sig"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
	payload, err := NewOAuthClient(httpClient, OAuthConfig{}).PollDevice(context.Background(), "dc")
	if err != nil {
		t.Fatal(err)
	}
	if payload.AccessToken != "at" || payload.RefreshToken != "rt" {
		t.Fatalf("payload=%#v", payload)
	}
}
