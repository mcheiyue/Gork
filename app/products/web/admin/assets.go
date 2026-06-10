package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
)

const adminAssetsPageSize = 2000

type adminAssetsRepository interface {
	ListAccounts(context.Context, adminAssetsListQuery) (adminAssetsListResult, error)
}

var (
	adminAssetsRepoProvider     = defaultAdminAssetsRepoProvider
	adminListAssets             = defaultAdminListAssets
	adminDeleteAsset            = defaultAdminDeleteAsset
	adminMarkInvalidCredentials = func(context.Context, adminAssetsRepository, string, error, string) bool {
		return false
	}
)

func handleAdminAssetsList(w http.ResponseWriter, r *http.Request) {
	repo, err := adminAssetsRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	tokens, err := listAdminAssetTokens(r.Context(), repo)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	rows, total := buildAdminAssetRows(r.Context(), repo, tokens)
	writeAdminJSON(w, http.StatusOK, map[string]any{"tokens": rows, "total_assets": total})
}

func handleAdminAssetDeleteItem(w http.ResponseWriter, r *http.Request) {
	repo, err := adminAssetsRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, err := decodeAdminAssetDeleteRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	if err := adminDeleteAsset(r.Context(), req.Token, req.AssetID); err != nil {
		adminMarkInvalidCredentials(r.Context(), repo, req.Token, err, "asset delete")
		writeAdminError(w, adminAssetUpstreamError(err))
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success"})
}

func handleAdminAssetClearToken(w http.ResponseWriter, r *http.Request) {
	repo, err := adminAssetsRepo()
	if err != nil {
		writeAdminError(w, err)
		return
	}
	req, err := decodeAdminAssetClearRequest(r)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	deleted, err := clearAdminTokenAssets(r.Context(), repo, req.Token)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeAdminJSON(w, http.StatusOK, map[string]any{"status": "success", "deleted": deleted})
}

func buildAdminAssetRows(ctx context.Context, repo adminAssetsRepository, tokens []string) ([]map[string]any, int) {
	rows := make([]map[string]any, 0, len(tokens))
	total := 0
	for _, token := range tokens {
		row := fetchAdminAssetRow(ctx, repo, token)
		rows = append(rows, row)
		total += row["count"].(int)
	}
	return rows, total
}

func fetchAdminAssetRow(ctx context.Context, repo adminAssetsRepository, token string) map[string]any {
	resp, err := adminListAssets(ctx, token)
	if err != nil {
		adminMarkInvalidCredentials(ctx, repo, token, err, "asset list")
		return adminAssetRow(token, nil, err.Error())
	}
	return adminAssetRow(token, adminAssetItemsFromResponse(resp), "")
}

func clearAdminTokenAssets(ctx context.Context, repo adminAssetsRepository, token string) (int, error) {
	resp, err := adminListAssets(ctx, token)
	if err != nil {
		adminMarkInvalidCredentials(ctx, repo, token, err, "asset clear")
		return 0, adminAssetUpstreamError(err)
	}
	deleted, err := deleteAdminAssetItems(ctx, repo, token, adminAssetItemsFromResponse(resp))
	if err != nil {
		return deleted, adminAssetUpstreamError(err)
	}
	return deleted, nil
}

func deleteAdminAssetItems(ctx context.Context, repo adminAssetsRepository, token string, items []map[string]any) (int, error) {
	deleted := 0
	var firstMarked error
	for _, item := range items {
		assetID := adminAssetString(item, "id", "assetId")
		if assetID == "" {
			continue
		}
		if err := adminDeleteAsset(ctx, token, assetID); err != nil {
			if adminMarkInvalidCredentials(ctx, repo, token, err, "asset clear") && firstMarked == nil {
				firstMarked = err
			}
			continue
		}
		deleted++
	}
	return deleted, firstMarked
}

func listAdminAssetTokens(ctx context.Context, repo adminAssetsRepository) ([]string, error) {
	tokens := []string{}
	for page := 1; ; page++ {
		result, err := repo.ListAccounts(ctx, adminAssetsListQuery{Page: page, PageSize: adminAssetsPageSize})
		if err != nil {
			return nil, err
		}
		for _, account := range result.Items {
			if adminAssetAccountManageable(account) {
				tokens = append(tokens, account.Token)
			}
		}
		if adminAssetsLastPage(page, result) {
			return tokens, nil
		}
	}
}

func adminAssetAccountManageable(account adminAssetsAccount) bool {
	if account.Deleted || account.DeletedAt != nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(account.Status))
	if status == "" {
		status = "active"
	}
	return status == "active" || status == "cooling"
}

func adminAssetRow(token string, items []map[string]any, errorText string) map[string]any {
	assets := make([]map[string]any, 0, len(items))
	for _, item := range items {
		assets = append(assets, map[string]any{
			"id":           adminAssetString(item, "id", "assetId"),
			"name":         adminAssetString(item, "fileName", "name"),
			"file_path":    adminAssetString(item, "filePath", "file_path"),
			"content_type": adminAssetString(item, "contentType", "content_type"),
			"size":         adminAssetValue(item, "fileSize", "size", 0),
			"created_at":   adminAssetString(item, "createdAt", "created_at"),
		})
	}
	return map[string]any{
		"token": token, "masked": adminAssetMask(token),
		"count": len(assets), "assets": assets, "error": adminErrorValue(errorText),
	}
}

func adminAssetItemsFromResponse(resp map[string]any) []map[string]any {
	raw := adminAssetValue(resp, "assets", "items", []any{})
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]any); ok {
			items = append(items, item)
		}
	}
	return items
}

func decodeAdminAssetDeleteRequest(r *http.Request) (adminAssetDeleteRequest, error) {
	var req adminAssetDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	return req, validateAdminAssetRequest(req.Token, true, req.AssetID)
}

func decodeAdminAssetClearRequest(r *http.Request) (adminAssetClearRequest, error) {
	var req adminAssetClearRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, platform.NewValidationError("Invalid JSON body", "body", "invalid_json")
	}
	return req, validateAdminAssetRequest(req.Token, false, "")
}

type adminAssetDeleteRequest struct {
	Token   string `json:"token"`
	AssetID string `json:"asset_id"`
}

type adminAssetClearRequest struct {
	Token string `json:"token"`
}

func validateAdminAssetRequest(token string, requireAsset bool, assetID string) error {
	if strings.TrimSpace(token) == "" {
		return platform.NewValidationError("token is required", "token", "")
	}
	if requireAsset && strings.TrimSpace(assetID) == "" {
		return platform.NewValidationError("asset_id is required", "asset_id", "")
	}
	return nil
}

func adminAssetsRepo() (adminAssetsRepository, error) {
	if repo := adminAssetsRepoProvider(); repo != nil {
		return repo, nil
	}
	return nil, platform.NewAppError("Account repository is not initialised", platform.ErrorKindServer, "account_repository_not_initialised", 500, nil)
}

func adminAssetValue(item map[string]any, first string, second string, fallback any) any {
	if value, ok := item[first]; ok && value != nil {
		return value
	}
	if value, ok := item[second]; ok && value != nil {
		return value
	}
	return fallback
}

func adminAssetString(item map[string]any, first string, second string) string {
	value := adminAssetValue(item, first, second, "")
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func adminAssetMask(token string) string {
	if len(token) <= 20 {
		return token
	}
	return token[:8] + "..." + token[len(token)-8:]
}

func adminErrorValue(errorText string) any {
	if errorText == "" {
		return nil
	}
	return errorText
}

func adminAssetUpstreamError(err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	return platform.NewUpstreamError(err.Error(), 502, err.Error())
}
