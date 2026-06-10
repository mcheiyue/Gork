package transport

import (
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	proxyadapters "github.com/jiujiu532/grok2api/app/dataplane/proxy/adapters"
)

func isAssetURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func responseContentType(headers map[string]string) string {
	for key, value := range headers {
		if strings.EqualFold(key, "content-type") {
			contentType := strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
			if contentType != "" {
				return contentType
			}
		}
	}
	return "application/octet-stream"
}

func mimeFromName(filename string, fallback string) string {
	contentType := strings.TrimSpace(mime.TypeByExtension(filepath.Ext(filename)))
	if contentType != "" {
		return strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	return fallback
}

func filenameFromURL(fileURL string) string {
	raw := strings.SplitN(fileURL, "?", 2)[0]
	last := raw[strings.LastIndex(raw, "/")+1:]
	if last == "" {
		return "download"
	}
	return last
}

func extractUserID(token string) string {
	match := xUserIDPattern.FindStringSubmatch(proxyadapters.BuildSSOCookie(token))
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func stripWhitespace(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func truncateString(value string, limit int) string {
	if len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
