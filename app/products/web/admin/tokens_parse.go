package admin

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

func adminTokenSanitize(value string) string {
	replacer := strings.NewReplacer("‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "−", "-", "\u00a0", " ", "\u2007", " ", "\u202f", " ", "\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "")
	token := replacer.Replace(value)
	token = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		if r > 127 {
			return -1
		}
		return r
	}, token)
	if strings.HasPrefix(token, "sso=") {
		return token[4:]
	}
	return token
}

func adminTokenDedupe(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		token := adminTokenSanitize(value)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func adminTokenTags(raw any) []string {
	var parts []string
	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		parts = strings.Split(value, ",")
	case []string:
		parts = value
	case []any:
		for _, item := range value {
			parts = append(parts, fmt.Sprint(item))
		}
	default:
		parts = []string{fmt.Sprint(value)}
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func adminTokensFromText(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == '\r' || r == '\n' || r == ',' })
	return adminTokenDedupe(fields)
}

func adminTokenStringSlice(raw []any) []string {
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func adminTokensPoolPayloadFromJSON(raw string) (map[string][]adminTokensUpsert, error) {
	parsed := map[string][]any{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, adminValidation("Invalid JSON import file", "file")
	}
	payload := map[string][]adminTokensUpsert{}
	for pool, items := range parsed {
		upserts := adminTokensUpsertsFromItems(pool, items)
		if len(upserts) > 0 {
			payload[adminTokenPool(pool)] = upserts
		}
	}
	if len(payload) == 0 {
		return nil, adminValidation("No valid tokens provided", "file")
	}
	return payload, nil
}

func adminTokensUpsertsFromItems(pool string, items []any) []adminTokensUpsert {
	upserts := []adminTokensUpsert{}
	for _, item := range items {
		upsert := adminTokenUpsertFromItem(pool, item)
		if upsert.Token != "" {
			upserts = append(upserts, upsert)
		}
	}
	return upserts
}

func adminTokenUpsertFromItem(pool string, item any) adminTokensUpsert {
	switch value := item.(type) {
	case string:
		return adminTokensUpsert{Token: adminTokenSanitize(value), Pool: adminTokenPool(pool)}
	case map[string]any:
		token, ok := value["token"]
		if !ok || token == nil {
			return adminTokensUpsert{}
		}
		return adminTokensUpsert{Token: adminTokenSanitize(fmt.Sprint(token)), Pool: adminTokenPool(pool), Tags: adminTokenTags(value["tags"])}
	default:
		return adminTokensUpsert{Token: adminTokenSanitize(fmt.Sprint(value)), Pool: adminTokenPool(pool)}
	}
}

func adminTokenPool(pool string) string {
	value := strings.ToLower(strings.TrimSpace(pool))
	if value == "" {
		return "basic"
	}
	return value
}
