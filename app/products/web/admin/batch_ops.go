package admin

import (
	"context"

	"github.com/jiujiu532/grok2api/app/platform"
)

func adminBatchNSFWOne(ctx context.Context, repo adminBatchRepository, token string, enabled bool) (map[string]any, error) {
	if enabled {
		if err := adminBatchNSFWSequence(ctx, token); err != nil {
			return nil, err
		}
	} else {
		if err := adminBatchSetNSFW(ctx, token, enabled); err != nil {
			return nil, err
		}
	}
	patch := adminBatchNSFWPatch(token, enabled)
	if _, err := repo.PatchAccounts(ctx, []adminBatchAccountPatch{patch}); err != nil {
		return nil, err
	}
	return map[string]any{"success": true, "tagged": enabled}, nil
}

func adminBatchNSFWPatch(token string, enabled bool) adminBatchAccountPatch {
	if enabled {
		return adminBatchAccountPatch{Token: token, AddTags: []string{"nsfw"}}
	}
	return adminBatchAccountPatch{Token: token, RemoveTags: []string{"nsfw"}}
}

func adminBatchRefreshOne(ctx context.Context, service adminBatchRefreshService, token string) (map[string]any, error) {
	result, err := service.RefreshTokens(ctx, []string{token})
	if err != nil {
		return nil, err
	}
	if result.Refreshed == 0 {
		return nil, platform.NewUpstreamError("未获取到真实配额数据", 502, "")
	}
	return map[string]any{"refreshed": result.Refreshed}, nil
}

func adminBatchCacheClearOne(ctx context.Context, repo adminAssetsRepository, token string) (map[string]any, error) {
	resp, err := adminListAssets(ctx, token)
	if err != nil {
		adminMarkInvalidCredentials(ctx, repo, token, err, "asset batch clear")
		return nil, err
	}
	deleted, err := deleteAdminBatchAssetItems(ctx, repo, token, adminAssetItemsFromResponse(resp))
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": deleted}, nil
}

func deleteAdminBatchAssetItems(ctx context.Context, repo adminAssetsRepository, token string, items []map[string]any) (int, error) {
	deleted := 0
	var firstMarked error
	for _, item := range items {
		assetID := adminAssetString(item, "id", "assetId")
		if assetID == "" {
			continue
		}
		if err := adminDeleteAsset(ctx, token, assetID); err != nil {
			if adminMarkInvalidCredentials(ctx, repo, token, err, "asset batch clear") && firstMarked == nil {
				firstMarked = err
			}
			continue
		}
		deleted++
	}
	if firstMarked != nil {
		return deleted, firstMarked
	}
	return deleted, nil
}
