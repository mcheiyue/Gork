package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jiujiu532/grok2api/app/platform/auth"
	"github.com/jiujiu532/grok2api/app/platform/config"
	"github.com/jiujiu532/grok2api/app/products/openai"
)

func parseWebUIImagineStart(payload map[string]any) (webUIImagineStart, bool) {
	prompt := strings.TrimSpace(stringValue(payload["prompt"]))
	if prompt == "" {
		return webUIImagineStart{}, false
	}
	quality := strings.ToLower(strings.TrimSpace(stringValueWithDefault(payload["quality"], "speed")))
	if quality != "speed" && quality != "quality" {
		quality = "speed"
	}
	nsfw := parseOptionalBool(payload["nsfw"])
	if nsfw == nil {
		value := webUIImagineDefaultNSFW()
		nsfw = &value
	}
	return webUIImagineStart{
		Prompt: prompt, AspectRatio: openai.ResolveAspectRatio(stringValueWithDefault(payload["aspect_ratio"], "2:3")),
		Count: clampWebUIImagineCount(payload["count"]), Quality: quality, EnableNSFW: nsfw, EnablePro: quality == "quality",
	}, true
}

func (start webUIImagineStart) webOptions() webUIImagineOptions {
	return webUIImagineOptions{
		AspectRatio: start.AspectRatio,
		Count:       start.Count,
		EnableNSFW:  start.EnableNSFW,
		EnablePro:   start.EnablePro,
	}
}

func webUIImagineRunning(runID string, start webUIImagineStart) map[string]any {
	return map[string]any{
		"type": "status", "status": "running", "prompt": start.Prompt,
		"aspect_ratio": start.AspectRatio, "run_id": runID,
		"count": start.Count, "quality": start.Quality,
	}
}

func copyImagineEvent(event map[string]any, runID string) map[string]any {
	out := make(map[string]any, len(event)+1)
	for key, value := range event {
		out[key] = value
	}
	if _, ok := out["run_id"]; !ok {
		out["run_id"] = runID
	}
	return out
}

func webUIImagineAllowed(r *http.Request) bool {
	settings := webUIAuthSettings()
	key := strings.TrimSpace(auth.GetWebUIKey(settings))
	if key == "" {
		return auth.IsWebUIEnabled(settings)
	}
	token := webUIImagineToken(r)
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(key)) == 1
}

func webUIImagineToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header != "" {
		scheme, token, found := strings.Cut(header, " ")
		if found && strings.EqualFold(scheme, "bearer") && strings.TrimSpace(token) != "" {
			return strings.TrimSpace(token)
		}
		return header
	}
	return strings.TrimSpace(r.URL.Query().Get("access_token"))
}

func parseOptionalBool(value any) *bool {
	if value == nil {
		return nil
	}
	parsed := false
	switch typed := value.(type) {
	case bool:
		parsed = typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			parsed = true
		}
	default:
		parsed = fmt.Sprint(value) != "" && fmt.Sprint(value) != "0"
	}
	return &parsed
}

func clampWebUIImagineCount(value any) int {
	count := intFromAny(value, 6)
	if count < 1 {
		return 1
	}
	if count > 6 {
		return 6
	}
	return count
}

func intFromAny(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func stringValueWithDefault(value any, fallback string) string {
	raw := strings.TrimSpace(stringValue(value))
	if raw == "" {
		return fallback
	}
	return raw
}

func defaultWebUIImagineEvents(ctx context.Context, prompt string, options webUIImagineOptions) ([]map[string]any, bool, error) {
	return openai.StreamImagineEvents(ctx, prompt, openai.ImagineEventOptions{
		AspectRatio: options.AspectRatio,
		Count:       options.Count,
		EnableNSFW:  options.EnableNSFW,
		EnablePro:   options.EnablePro,
	})
}

func defaultWebUIImagineDefaultNSFW() bool {
	return boolFromAny(config.GetConfig("features.enable_nsfw", true), true)
}

func defaultWebUIImagineRunID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(raw[:])
}

func boolFromAny(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return fallback
}
