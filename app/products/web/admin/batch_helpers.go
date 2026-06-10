package admin

import (
	"context"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
)

func adminBatchTokensOrAll(ctx context.Context, repo adminAssetsRepository, raw []string, emptyMessage string) ([]string, error) {
	tokens := adminBatchTrimTokens(raw)
	var err error
	if len(tokens) == 0 {
		tokens, err = listAdminAssetTokens(ctx, repo)
	}
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, platform.NewValidationError(emptyMessage, "tokens", "")
	}
	return tokens, nil
}

func adminBatchTrimTokens(raw []string) []string {
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		if trimmed := strings.TrimSpace(token); trimmed != "" {
			tokens = append(tokens, trimmed)
		}
	}
	return tokens
}

func adminBatchRepo() (adminBatchRepository, error) {
	if repo := adminBatchRepoProvider(); repo != nil {
		return repo, nil
	}
	return nil, platform.NewAppError("Account repository is not initialised", platform.ErrorKindServer, "account_repository_not_initialised", 500, nil)
}

func adminBatchRefreshSvc() (adminBatchRefreshService, error) {
	if service := adminBatchRefreshServiceProvider(); service != nil {
		return service, nil
	}
	return nil, platform.NewAppError("Refresh service is not initialised", platform.ErrorKindServer, "refresh_service_not_initialised", 500, nil)
}

func maxAdminBatchInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
