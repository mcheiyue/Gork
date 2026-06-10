package admin

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/jiujiu532/grok2api/app/platform"
)

var startupOnlyConfigPrefixes = []string{
	"account.storage", "account.local", "account.redis", "account.mysql", "account.postgresql",
}

var configCharReplacements = map[rune]rune{
	'\u2010': '-', '\u2011': '-', '\u2012': '-', '\u2013': '-', '\u2014': '-', '\u2212': '-',
	'\u2018': '\'', '\u2019': '\'', '\u201c': '"', '\u201d': '"',
	'\u00a0': ' ', '\u2007': ' ', '\u202f': ' ',
	'\u200b': 0, '\u200c': 0, '\u200d': 0, '\ufeff': 0,
}

func sanitizeText(value any, removeAllSpaces bool) string {
	text := ""
	if value != nil {
		text = fmt.Sprint(value)
	}
	text = strings.Map(replaceConfigRune, text)
	if removeAllSpaces {
		text = removeSpaces(text)
	} else {
		text = strings.TrimSpace(text)
	}
	return latin1Only(text)
}

func replaceConfigRune(r rune) rune {
	if replacement, ok := configCharReplacements[r]; ok {
		if replacement == 0 {
			return -1
		}
		return replacement
	}
	return r
}

func removeSpaces(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

func latin1Only(text string) string {
	return strings.Map(func(r rune) rune {
		if r > 255 {
			return -1
		}
		return r
	}, text)
}

func sanitizeProxyConfig(payload map[string]any) map[string]any {
	proxy, ok := payload["proxy"].(map[string]any)
	if !ok {
		return cloneAdminMap(payload)
	}
	sanitized, changed := sanitizeProxyFields(proxy)
	if clearance, ok := sanitized["clearance"].(map[string]any); ok {
		cleaned, clearanceChanged := sanitizeProxyFields(clearance)
		if clearanceChanged {
			sanitized["clearance"] = cleaned
			changed = true
		}
	}
	if !changed {
		return cloneAdminMap(payload)
	}
	result := cloneAdminMap(payload)
	result["proxy"] = sanitized
	return result
}

func sanitizeProxyFields(target map[string]any) (map[string]any, bool) {
	normalized := cloneAdminMap(target)
	changed := false
	for _, field := range []struct {
		key        string
		stripSpace bool
	}{{"user_agent", false}, {"cf_cookies", false}, {"cf_clearance", true}} {
		raw, ok := normalized[field.key]
		if !ok {
			continue
		}
		value := sanitizeText(raw, field.stripSpace)
		if value != raw {
			normalized[field.key] = value
			changed = true
		}
	}
	return normalized, changed
}

func ensureRuntimePatchAllowed(payload map[string]any) error {
	for _, path := range iterPatchPaths(payload, "") {
		for _, blocked := range startupOnlyConfigPrefixes {
			if path == blocked || strings.HasPrefix(path, blocked+".") {
				return platform.NewValidationError("Storage config is startup-only and must be set via env", path, "startup_only_config")
			}
		}
	}
	return nil
}

func patchTouchesPrefix(payload map[string]any, prefix string) bool {
	for _, path := range iterPatchPaths(payload, "") {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}

func iterPatchPaths(value any, prefix string) []string {
	mapped, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	paths := []string{}
	for key, child := range mapped {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if _, ok := child.(map[string]any); ok {
			paths = append(paths, iterPatchPaths(child, path)...)
		} else {
			paths = append(paths, path)
		}
	}
	return paths
}

func cloneAdminMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
