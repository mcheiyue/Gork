package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse"
	"github.com/jiujiu532/grok2api/app/platform/auth"
)

func TestWebUIVoiceTokenRequiresKey(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }

	rec := webUIRequest(http.MethodPost, "/webui/api/voice/token", `{}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = webUIRequest(http.MethodGet, "/webui/api/voice/token", "", "Bearer web")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebUIVoiceTokenDirectoryNotInitialised(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIVoiceDirectory = func() webUIVoiceAccountDirectory { return nil }

	rec := webUIRequest(http.MethodPost, "/webui/api/voice/token", `{}`, "Bearer web")
	assertWebUIErrorContains(t, rec, http.StatusTooManyRequests, "Account directory not initialised")
}

func TestWebUIVoiceTokenReservesAccountAndReturnsPythonShape(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	dir := &fakeWebUIVoiceDirectory{lease: &reverse.AccountLease{Idx: 3, Token: "sso-token"}}
	webUIVoiceDirectory = func() webUIVoiceAccountDirectory { return dir }

	var gotToken string
	var gotOptions webUIVoiceOptions
	webUIVoiceFetchToken = func(_ context.Context, token string, options webUIVoiceOptions) (map[string]any, error) {
		gotToken, gotOptions = token, options
		return map[string]any{
			"token":            "lk-token",
			"participant_name": "participant-1",
			"room":             "room-1",
		}, nil
	}

	rec := webUIRequest(http.MethodPost, "/webui/api/voice/token", `{"voice":"juniper","personality":"coach","speed":1.25,"instruction":"  be brief  "}`, "Bearer web")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeWebUIBody(t, rec)
	if body["token"] != "lk-token" || body["url"] != "wss://livekit.grok.com" {
		t.Fatalf("token/url body=%#v", body)
	}
	if body["participant_name"] != "participant-1" || body["room_name"] != "room-1" {
		t.Fatalf("participant/room body=%#v", body)
	}
	if gotToken != "sso-token" || gotOptions.Voice != "juniper" || gotOptions.Personality != "coach" {
		t.Fatalf("transport token/options=%q/%#v", gotToken, gotOptions)
	}
	if gotOptions.Speed != 1.25 || gotOptions.CustomInstruction != "be brief" {
		t.Fatalf("transport numeric/instruction=%#v", gotOptions)
	}
	if dir.releases != 1 || dir.reservePool[0] != 1 || dir.reservePool[1] != 2 || dir.reserveMode != controlmodel.ModeAuto {
		t.Fatalf("directory reserve/release=%#v mode=%d releases=%d", dir.reservePool, dir.reserveMode, dir.releases)
	}
}

func TestWebUIVoiceTokenResponseAliasesAndDefaults(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIVoiceDirectory = func() webUIVoiceAccountDirectory {
		return &fakeWebUIVoiceDirectory{lease: &reverse.AccountLease{Token: "sso-token"}}
	}
	webUIVoiceFetchToken = func(context.Context, string, webUIVoiceOptions) (map[string]any, error) {
		return map[string]any{
			"token":           "lk-token",
			"livekitUrl":      "wss://custom",
			"participantName": "participant-camel",
			"roomName":        "room-camel",
		}, nil
	}

	rec := webUIRequest(http.MethodPost, "/webui/api/voice/token", `{}`, "Bearer web")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeWebUIBody(t, rec)
	if body["url"] != "wss://custom" || body["participant_name"] != "participant-camel" || body["room_name"] != "room-camel" {
		t.Fatalf("camel aliases body=%#v", body)
	}

	webUIVoiceFetchToken = func(context.Context, string, webUIVoiceOptions) (map[string]any, error) {
		return map[string]any{"token": "lk-token"}, nil
	}
	rec = webUIRequest(http.MethodPost, "/webui/api/voice/token", `{}`, "Bearer web")
	body = decodeWebUIBody(t, rec)
	if body["participant_name"] != "" || body["room_name"] != "" {
		t.Fatalf("empty defaults body=%#v", body)
	}
}

func TestWebUIVoiceTokenDefaultsAndErrors(t *testing.T) {
	resetWebUITestDeps(t)
	webUIAuthSettings = func() auth.AuthSettings { return auth.AuthSettings{WebUIKey: "web"} }
	webUIVoiceDirectory = func() webUIVoiceAccountDirectory { return &fakeWebUIVoiceDirectory{} }

	rec := webUIRequest(http.MethodPost, "/webui/api/voice/token", `{}`, "Bearer web")
	assertWebUIErrorContains(t, rec, http.StatusTooManyRequests, "No available tokens for voice mode")

	dir := &fakeWebUIVoiceDirectory{lease: &reverse.AccountLease{Token: "sso-token"}}
	webUIVoiceDirectory = func() webUIVoiceAccountDirectory { return dir }
	webUIVoiceFetchToken = func(_ context.Context, _ string, options webUIVoiceOptions) (map[string]any, error) {
		if options.Voice != "ara" || options.Personality != "assistant" || options.Speed != 1.0 {
			t.Fatalf("default options=%#v", options)
		}
		return map[string]any{"livekitUrl": "wss://custom"}, nil
	}

	rec = webUIRequest(http.MethodPost, "/webui/api/voice/token", `{}`, "Bearer web")
	assertWebUIErrorContains(t, rec, http.StatusBadGateway, "Upstream returned no voice token")
}

func assertWebUIErrorContains(t *testing.T, rec *httptest.ResponseRecorder, status int, text string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), text) {
		t.Fatalf("status/body=%d/%s, want status=%d containing %q", rec.Code, rec.Body.String(), status, text)
	}
}

type fakeWebUIVoiceDirectory struct {
	lease       *reverse.AccountLease
	reservePool []int
	reserveMode controlmodel.ModeID
	releases    int
}

func (d *fakeWebUIVoiceDirectory) Reserve(_ context.Context, poolCandidates []int, modeID int) (*reverse.AccountLease, error) {
	d.reservePool = append([]int(nil), poolCandidates...)
	d.reserveMode = controlmodel.ModeID(modeID)
	if d.lease == nil {
		return nil, nil
	}
	return d.lease, nil
}

func (d *fakeWebUIVoiceDirectory) Release(context.Context, reverse.AccountLease) error {
	d.releases++
	return nil
}
