package webui

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/auth"
)

func webUIProtected(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeWebUIJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"message": "Method not allowed"}})
			return
		}
		if err := auth.VerifyWebUIKey(r.Header.Get("Authorization"), webUIAuthSettings()); err != nil {
			writeWebUIError(w, err)
			return
		}
		handler(w, r)
	}
}

func writeWebUIJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeWebUIError(w http.ResponseWriter, err error) {
	var validation *platform.ValidationError
	if errors.As(err, &validation) {
		writeWebUIJSON(w, validation.Status, validation.ToDict())
		return
	}
	var rateLimit *platform.RateLimitError
	if errors.As(err, &rateLimit) {
		writeWebUIJSON(w, rateLimit.Status, rateLimit.ToDict())
		return
	}
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		writeWebUIJSON(w, upstream.Status, upstream.ToDict())
		return
	}
	var appErr *platform.AppError
	if errors.As(err, &appErr) {
		writeWebUIJSON(w, appErr.Status, appErr.ToDict())
		return
	}
	writeWebUIJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": err.Error()}})
}
