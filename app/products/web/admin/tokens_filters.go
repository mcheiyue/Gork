package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
)

func adminValidation(message string, param string) error {
	return platform.NewValidationError(message, param, "")
}

func adminTokensListQuery(r *http.Request) (adminAssetsListQuery, error) {
	query := r.URL.Query()
	nsfwTags, excludeTags, err := adminTokensNSFWTags(query.Get("nsfw"))
	if err != nil {
		return adminAssetsListQuery{}, err
	}
	status, err := adminTokensStatusFilter(query.Get("status"))
	if err != nil {
		return adminAssetsListQuery{}, err
	}
	return adminAssetsListQuery{
		Page: adminTokensPositiveQuery(r, "page", 1), PageSize: adminTokensPositiveQuery(r, "page_size", 50),
		Pool: adminTokensPoolFilter(query.Get("pool")), Status: status,
		Tags: nsfwTags, ExcludeTags: excludeTags,
		SortBy:   adminTokensQueryDefault(query.Get("sort_by"), "updated_at"),
		SortDesc: adminTokensBoolQuery(query.Get("sort_desc"), true),
	}, nil
}

func adminTokensPositiveQuery(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func adminTokensBoolQuery(raw string, fallback bool) bool {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func adminTokensQueryDefault(raw string, fallback string) string {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	return raw
}

func adminTokensPoolFilter(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == "all" {
		return ""
	}
	return value
}

func adminTokensStatusFilter(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == "all" {
		return "", nil
	}
	if value == "invalid" {
		return "expired", nil
	}
	switch value {
	case "active", "cooling", "expired", "disabled":
		return value, nil
	default:
		return "", adminValidation("Invalid status filter", "status")
	}
}

func adminTokensNSFWTags(raw string) ([]string, []string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "all":
		return nil, nil, nil
	case "enabled":
		return []string{"nsfw"}, nil, nil
	case "disabled":
		return nil, []string{"nsfw"}, nil
	default:
		return nil, nil, adminValidation("Invalid NSFW filter", "nsfw")
	}
}
