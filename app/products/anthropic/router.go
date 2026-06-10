package anthropic

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/auth"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

var (
	anthropicRouterMessages     = Messages
	anthropicRouterAuthSettings = func() auth.AuthSettings {
		return auth.AuthSettings{APIKey: config.GetConfig("app.api_key", "")}
	}
	anthropicRouterBoolConfig = func(key string, defaultValue bool) bool {
		return anthropicBoolConfig(key, defaultValue)
	}
)

// NewRouter returns the Anthropic-compatible /v1 HTTP surface.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", anthropicProtected(http.MethodPost, handleAnthropicMessages))
	return mux
}

func anthropicMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeAnthropicJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{
				"message": "Method not allowed", "type": "invalid_request_error",
			}})
			return
		}
		handler(w, r)
	}
}

func anthropicProtected(method string, handler http.HandlerFunc) http.HandlerFunc {
	return anthropicMethod(method, func(w http.ResponseWriter, r *http.Request) {
		err := auth.VerifyAPIKey(r.Header.Get("Authorization"), r.Header.Get("x-api-key"), anthropicRouterAuthSettings())
		if err != nil {
			writeAnthropicError(w, err)
			return
		}
		handler(w, r)
	})
}

func writeAnthropicJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAnthropicError(w http.ResponseWriter, err error) {
	var validation *platform.ValidationError
	if errors.As(err, &validation) && validation.AppError != nil {
		writeAnthropicJSON(w, validation.Status, validation.ToDict())
		return
	}
	var appErr *platform.AppError
	if errors.As(err, &appErr) && appErr != nil {
		writeAnthropicJSON(w, appErr.Status, appErr.ToDict())
		return
	}
	writeAnthropicJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{
		"message": err.Error(), "type": "server_error", "code": "internal_error",
	}})
}

func writeAnthropicStream(w http.ResponseWriter, frames []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	for _, frame := range frames {
		_, _ = w.Write([]byte(frame))
	}
}

func writeAnthropicResult(w http.ResponseWriter, result MessagesResult) {
	if result.IsStream {
		writeAnthropicStream(w, result.StreamFrames)
		return
	}
	writeAnthropicJSON(w, http.StatusOK, result.Response)
}

func anthropicBoolConfig(key string, defaultValue bool) bool {
	value := config.GetConfig(key, defaultValue)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "1" || typed == "true" || typed == "yes" || typed == "on"
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case nil:
		return defaultValue
	default:
		return true
	}
}

func anthropicFloatDefault(value *float64, defaultValue float64) float64 {
	if value == nil {
		return defaultValue
	}
	return *value
}
