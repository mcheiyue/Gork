package auth

import (
	"errors"
	"reflect"
	"testing"

	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestAuthSettingsHelpersMatchPython(t *testing.T) {
	if got := GetAPIKeys(AuthSettings{APIKey: " one, two ,, "}); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("string API keys = %#v", got)
	}
	if got := GetAPIKeys(AuthSettings{APIKey: []any{" a ", "", 2}}); !reflect.DeepEqual(got, []string{"a", "2"}) {
		t.Fatalf("list API keys = %#v", got)
	}
	if GetAdminKey(AuthSettings{}) != "grok2api" || GetAdminKey(AuthSettings{AdminKey: ""}) != "" {
		t.Fatalf("admin key defaults mismatch")
	}
	if GetWebUIKey(AuthSettings{WebUIKey: 123}) != "123" {
		t.Fatalf("webui key conversion mismatch")
	}
	for _, value := range []any{true, "1", "true", "YES", "on", 1} {
		if !IsWebUIEnabled(AuthSettings{WebUIEnabled: value}) {
			t.Fatalf("webui enabled value %#v should be true", value)
		}
	}
	for _, value := range []any{false, "0", "false", "", nil} {
		if IsWebUIEnabled(AuthSettings{WebUIEnabled: value}) {
			t.Fatalf("webui enabled value %#v should be false", value)
		}
	}
	if token, ok := ExtractBearer("Bearer secret"); !ok || token != "secret" {
		t.Fatalf("bearer token = %q ok=%v", token, ok)
	}
	if _, ok := ExtractBearer("Basic secret"); ok {
		t.Fatalf("basic auth should not parse as bearer")
	}
}

func TestVerifyAPIKeyAllowsDisabledBearerAndXAPIKey(t *testing.T) {
	if err := VerifyAPIKey("", "", AuthSettings{}); err != nil {
		t.Fatalf("disabled API key should allow request: %v", err)
	}
	settings := AuthSettings{APIKey: "alpha,beta"}
	if err := VerifyAPIKey("Bearer alpha", "", settings); err != nil {
		t.Fatalf("bearer key rejected: %v", err)
	}
	if err := VerifyAPIKey("", "beta", settings); err != nil {
		t.Fatalf("x-api-key rejected: %v", err)
	}
	assertAuthError(t, VerifyAPIKey("", "", settings), 401, "Missing or invalid Authorization header.")
	assertAuthError(t, VerifyAPIKey("Bearer wrong", "", settings), 403, "Invalid API key.")
}

func TestVerifyAdminKeyMatchesHeaderAndQueryBehavior(t *testing.T) {
	settings := AuthSettings{AdminKey: "admin"}
	if err := VerifyAdminKey("Bearer admin", "", settings); err != nil {
		t.Fatalf("admin bearer rejected: %v", err)
	}
	if err := VerifyAdminKey("", "admin", settings); err != nil {
		t.Fatalf("admin query key rejected: %v", err)
	}
	assertAuthError(t, VerifyAdminKey("", "", AuthSettings{AdminKey: ""}), 401, "Admin key is not configured.")
	assertAuthError(t, VerifyAdminKey("", "", settings), 401, "Missing authentication token.")
	assertAuthError(t, VerifyAdminKey("Bearer wrong", "", settings), 401, "Invalid authentication token.")
}

func TestVerifyWebUIKeyMatchesEnabledAndBearerBehavior(t *testing.T) {
	if err := VerifyWebUIKey("", AuthSettings{WebUIEnabled: true}); err != nil {
		t.Fatalf("enabled webui without key should allow request: %v", err)
	}
	assertAuthError(t, VerifyWebUIKey("", AuthSettings{}), 401, "WebUI access is disabled.")

	settings := AuthSettings{WebUIKey: "web"}
	if err := VerifyWebUIKey("Bearer web", settings); err != nil {
		t.Fatalf("webui bearer rejected: %v", err)
	}
	assertAuthError(t, VerifyWebUIKey("", settings), 401, "Missing authentication token.")
	assertAuthError(t, VerifyWebUIKey("Bearer wrong", settings), 401, "Invalid authentication token.")
}

func assertAuthError(t *testing.T, err error, status int, message string) {
	t.Helper()
	var appErr *platform.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %T %v, want *platform.AppError", err, err)
	}
	if appErr.Status != status || appErr.Message != message {
		t.Fatalf("auth error = %#v, want status=%d message=%q", appErr, status, message)
	}
}
