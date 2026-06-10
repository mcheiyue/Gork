package admin

import (
	"context"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/transport"
)

func defaultAdminListAssets(ctx context.Context, token string) (map[string]any, error) {
	return transport.ListAssets(ctx, token, nil)
}

func defaultAdminDeleteAsset(ctx context.Context, token string, assetID string) error {
	_, err := transport.DeleteAsset(ctx, token, assetID)
	return err
}
