package admin

import (
	"context"
	"strings"

	"github.com/dslzl/gork/app/platform"
)

type adminBatchAccountGetter interface {
	GetAccounts(context.Context, []string) ([]adminAssetsAccount, error)
}

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

func adminBatchRefreshTokens(ctx context.Context, repo adminAssetsRepository, raw []string, allManageable bool) ([]string, error) {
	if allManageable {
		return adminBatchTokensOrAll(ctx, repo, nil, "No manageable tokens available")
	}
	tokens := adminBatchTrimTokens(raw)
	if len(tokens) == 0 {
		return nil, platform.NewValidationError("No tokens provided", "tokens", "")
	}
	return adminBatchFilterManageableTokens(ctx, repo, tokens, "No manageable tokens available")
}

func adminBatchFilterManageableTokens(ctx context.Context, repo adminAssetsRepository, tokens []string, emptyMessage string) ([]string, error) {
	if getter, ok := repo.(adminBatchAccountGetter); ok {
		return adminBatchFilterManageableByLookup(ctx, getter, tokens, emptyMessage)
	}
	available, err := listAdminAssetTokens(ctx, repo)
	if err != nil {
		return nil, err
	}
	availableSet := make(map[string]struct{}, len(available))
	for _, token := range available {
		availableSet[token] = struct{}{}
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, ok := availableSet[token]; ok {
			filtered = append(filtered, token)
		}
	}
	if len(filtered) == 0 {
		return nil, platform.NewValidationError(emptyMessage, "tokens", "")
	}
	return filtered, nil
}

func adminBatchFilterManageableByLookup(ctx context.Context, getter adminBatchAccountGetter, tokens []string, emptyMessage string) ([]string, error) {
	records, err := getter.GetAccounts(ctx, tokens)
	if err != nil {
		return nil, err
	}
	byToken := make(map[string]adminAssetsAccount, len(records))
	for _, record := range records {
		byToken[record.Token] = record
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if record, ok := byToken[token]; ok && adminAssetAccountManageable(record) {
			filtered = append(filtered, token)
		}
	}
	if len(filtered) == 0 {
		return nil, platform.NewValidationError(emptyMessage, "tokens", "")
	}
	return filtered, nil
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

type adminBatchClassifiedError struct {
	class string
	err   error
}

func (e adminBatchClassifiedError) Error() string {
	return e.err.Error()
}

func adminBatchErrorClass(err error) string {
	if classified, ok := err.(adminBatchClassifiedError); ok {
		return classified.class
	}
	return ""
}

func adminBatchClassified(class string, err error) error {
	return adminBatchClassifiedError{class: class, err: err}
}
