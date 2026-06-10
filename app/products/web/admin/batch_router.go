package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
)

func handleAdminBatchNSFW(w http.ResponseWriter, r *http.Request) {
	repo, err := adminBatchRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, options, err := decodeAdminBatchRequest(r, "batch.nsfw_concurrency")
	if err != nil {
		writeAdminError(w, err)
		return
	}
	enabled, err := adminBatchBoolQuery(r, "enabled", true)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	tokens, err := adminBatchTokensOrAll(r.Context(), repo, req.Tokens, "No tokens available")
	if err != nil {
		writeAdminError(w, err)
		return
	}
	handler := func(ctx context.Context, token string) (map[string]any, error) {
		return adminBatchNSFWOne(ctx, repo, token, enabled)
	}
	adminBatchDispatch(w, tokens, handler, options)
}

func handleAdminBatchRefresh(w http.ResponseWriter, r *http.Request) {
	service, err := adminBatchRefreshSvc()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, options, err := decodeAdminBatchRequest(r, "batch.refresh_concurrency")
	if err != nil {
		writeAdminError(w, err)
		return
	}
	tokens := adminBatchTrimTokens(req.Tokens)
	if len(tokens) == 0 {
		writeAdminError(w, platform.NewValidationError("No tokens provided", "tokens", ""))
		return
	}
	handler := func(ctx context.Context, token string) (map[string]any, error) {
		return adminBatchRefreshOne(ctx, service, token)
	}
	adminBatchDispatch(w, tokens, handler, options)
}

func handleAdminBatchCacheClear(w http.ResponseWriter, r *http.Request) {
	repo, err := adminAssetsRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, options, err := decodeAdminBatchRequest(r, "batch.asset_delete_concurrency")
	if err != nil {
		writeAdminError(w, err)
		return
	}
	tokens, err := adminBatchTokensOrAll(r.Context(), repo, req.Tokens, "No tokens available")
	if err != nil {
		writeAdminError(w, err)
		return
	}
	handler := func(ctx context.Context, token string) (map[string]any, error) {
		return adminBatchCacheClearOne(ctx, repo, token)
	}
	adminBatchDispatch(w, tokens, handler, options)
}

type adminBatchOptions struct {
	Async       bool
	Concurrency int
}

func decodeAdminBatchRequest(r *http.Request, concurrencyKey string) (adminBatchRequest, adminBatchOptions, error) {
	var req adminBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, adminBatchOptions{}, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	options, err := adminBatchOptionsFromQuery(r, concurrencyKey)
	return req, options, err
}

func adminBatchOptionsFromQuery(r *http.Request, concurrencyKey string) (adminBatchOptions, error) {
	asyncMode, err := adminBatchBoolQuery(r, "async", false)
	if err != nil {
		return adminBatchOptions{}, err
	}
	concurrency, err := adminBatchConcurrency(r, concurrencyKey)
	if err != nil {
		return adminBatchOptions{}, err
	}
	return adminBatchOptions{Async: asyncMode, Concurrency: concurrency}, nil
}

func adminBatchBoolQuery(r *http.Request, key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, platform.NewValidationError("Invalid boolean query parameter", key, "invalid_query")
	}
	return value, nil
}

func adminBatchConcurrency(r *http.Request, configKey string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("concurrency"))
	if raw == "" {
		return maxAdminBatchInt(1, adminBatchConfigInt(configKey, 50)), nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, platform.NewValidationError("concurrency must be >= 1", "concurrency", "invalid_query")
	}
	return value, nil
}
