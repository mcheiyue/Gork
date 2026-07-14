package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

// buildDeviceOAuth 管理面 Device 登录；可注入。
type buildDeviceOAuth interface {
	StartDevice(ctx context.Context) (build.DeviceAuthorization, error)
	PollDevice(ctx context.Context, deviceCode string) (build.TokenPayload, error)
}

var adminBuildOAuth = defaultAdminBuildOAuth

func defaultAdminBuildOAuth() buildDeviceOAuth {
	cfg := build.OAuthConfig{
		ClientID:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_client_id", build.DefaultOAuthClientID),
		Scope:     platformconfig.GlobalConfig.GetStr("provider.build.oauth_scope", build.DefaultOAuthScope),
		DeviceURL: platformconfig.GlobalConfig.GetStr("provider.build.oauth_device_url", build.DefaultDeviceURL),
		TokenURL:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_token_url", build.DefaultTokenURL),
	}
	return build.NewOAuthClient(nil, cfg)
}

// handleAdminBuildDeviceStart POST → 返回 user_code / verification_uri / device_code。
func handleAdminBuildDeviceStart(w http.ResponseWriter, r *http.Request) {
	oauth := adminBuildOAuth()
	authz, err := oauth.StartDevice(r.Context())
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"device_code":               authz.DeviceCode,
		"user_code":                 authz.UserCode,
		"verification_uri":          authz.VerificationURI,
		"verification_uri_complete": authz.VerificationURIComplete,
		"interval_seconds":          int(authz.Interval / time.Second),
		"expires_in_seconds":        int(authz.ExpiresIn / time.Second),
	})
}

// handleAdminBuildDevicePoll POST body: {device_code, name?} → pending / 成功落库。
func handleAdminBuildDevicePoll(w http.ResponseWriter, r *http.Request) {
	store, err := requireBuildAccountStore()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	var req struct {
		DeviceCode string `json:"device_code"`
		Name       string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAdminError(w, platform.NewValidationError("Invalid JSON body", "body", "invalid_json"))
		return
	}
	deviceCode := strings.TrimSpace(req.DeviceCode)
	if deviceCode == "" {
		writeAdminError(w, platform.NewValidationError("device_code is required", "device_code", ""))
		return
	}
	oauth := adminBuildOAuth()
	token, err := oauth.PollDevice(r.Context(), deviceCode)
	if err != nil {
		if build.IsAuthorizationPending(err) {
			writeAdminJSON(w, http.StatusOK, map[string]any{
				"status": "pending",
				"code":   "authorization_pending",
			})
			return
		}
		if build.IsSlowDown(err) {
			writeAdminJSON(w, http.StatusOK, map[string]any{
				"status": "slow_down",
				"code":   "slow_down",
			})
			return
		}
		writeAdminError(w, err)
		return
	}
	clientID := platformconfig.GlobalConfig.GetStr("provider.build.oauth_client_id", build.DefaultOAuthClientID)
	cred := build.CredentialFromToken(strings.TrimSpace(req.Name), token, clientID)
	acc, err := store.Upsert(r.Context(), buildaccount.FromCredential(cred))
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"account": serializeBuildAccount(acc),
	})
}
