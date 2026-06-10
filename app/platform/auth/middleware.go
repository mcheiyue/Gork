package auth

import (
	"crypto/subtle"
	"fmt"
	"strconv"
	"strings"

	platform "github.com/jiujiu532/grok2api/app/platform"
)

type AuthSettings struct {
	APIKey       any
	AdminKey     any
	WebUIKey     any
	WebUIEnabled any
}

func GetAPIKeys(settings AuthSettings) []string {
	raw := settings.APIKey
	if isEmptyAuthValue(raw) {
		return []string{}
	}
	switch typed := raw.(type) {
	case []string:
		return compactAuthStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, value := range typed {
			values = append(values, fmt.Sprint(value))
		}
		return compactAuthStrings(values)
	default:
		return compactAuthStrings(strings.Split(fmt.Sprint(raw), ","))
	}
}

func GetAdminKey(settings AuthSettings) string {
	if settings.AdminKey == nil {
		return "grok2api"
	}
	return fmt.Sprint(settings.AdminKey)
}

func GetWebUIKey(settings AuthSettings) string {
	if settings.WebUIKey == nil {
		return ""
	}
	return fmt.Sprint(settings.WebUIKey)
}

func IsWebUIEnabled(settings AuthSettings) bool {
	value := settings.WebUIEnabled
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	case nil:
		return false
	default:
		return authTruthyNumber(typed)
	}
}

func ExtractBearer(authorization string) (string, bool) {
	if authorization == "" {
		return "", false
	}
	scheme, token, ok := strings.Cut(authorization, " ")
	if !ok || strings.ToLower(scheme) != "bearer" || token == "" {
		return "", false
	}
	return token, true
}

func VerifyAPIKey(authorization, xAPIKey string, settings AuthSettings) error {
	allowed := GetAPIKeys(settings)
	if len(allowed) == 0 {
		return nil
	}
	token, ok := ExtractBearer(authorization)
	if !ok {
		token = xAPIKey
	}
	if token == "" {
		return authHTTPError(401, "Missing or invalid Authorization header.")
	}
	for _, key := range allowed {
		if constantTimeStringEqual(token, key) {
			return nil
		}
	}
	return authHTTPError(403, "Invalid API key.")
}

func VerifyAdminKey(authorization, appKey string, settings AuthSettings) error {
	key := GetAdminKey(settings)
	if key == "" {
		return authHTTPError(401, "Admin key is not configured.")
	}
	token, ok := ExtractBearer(authorization)
	if !ok {
		token = appKey
	}
	if token == "" {
		return authHTTPError(401, "Missing authentication token.")
	}
	if !constantTimeStringEqual(token, key) {
		return authHTTPError(401, "Invalid authentication token.")
	}
	return nil
}

func VerifyWebUIKey(authorization string, settings AuthSettings) error {
	webuiKey := GetWebUIKey(settings)
	if webuiKey == "" {
		if IsWebUIEnabled(settings) {
			return nil
		}
		return authHTTPError(401, "WebUI access is disabled.")
	}
	token, ok := ExtractBearer(authorization)
	if !ok || token == "" {
		return authHTTPError(401, "Missing authentication token.")
	}
	if !constantTimeStringEqual(token, webuiKey) {
		return authHTTPError(401, "Invalid authentication token.")
	}
	return nil
}

func compactAuthStrings(values []string) []string {
	out := []string{}
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func isEmptyAuthValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	return false
}

func authTruthyNumber(value any) bool {
	switch typed := value.(type) {
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	default:
		return fmt.Sprint(value) != "" && fmt.Sprint(value) != strconv.Itoa(0)
	}
}

func constantTimeStringEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func authHTTPError(status int, message string) *platform.AppError {
	return platform.NewAppError(message, platform.ErrorKindAuthentication, "authentication_error", status, nil)
}
