package admin

import "github.com/jiujiu532/grok2api/app/platform"

type adminTokensReplacePoolRequest struct {
	Pool   string   `json:"pool"`
	Tokens []string `json:"tokens"`
	Tags   []string `json:"tags"`
}

type adminTokensAddRequest struct {
	Tokens []string `json:"tokens"`
	Pool   string   `json:"pool"`
	Tags   []string `json:"tags"`
}

type adminTokensEditRequest struct {
	OldToken string `json:"old_token"`
	Token    string `json:"token"`
	Pool     string `json:"pool"`
}

type adminTokensToggleRequest struct {
	Token    string `json:"token"`
	Disabled bool   `json:"disabled"`
}

type adminTokensToggleBatchRequest struct {
	Tokens   []string `json:"tokens"`
	Disabled bool     `json:"disabled"`
}

type adminTokensImportRequest struct {
	Pool       string `json:"pool"`
	Mode       string `json:"mode"`
	TokensText string `json:"tokens_text"`
	Tokens     []any  `json:"tokens"`
	Tags       any    `json:"tags"`
}

func adminTokensRepo() (adminTokensRepository, error) {
	if repo := adminTokensRepoProvider(); repo != nil {
		return repo, nil
	}
	return nil, platform.NewAppError("Account repository is not initialised", platform.ErrorKindServer, "account_repository_not_initialised", 500, nil)
}

func adminTokensRefreshSvc() (adminTokensRefreshService, error) {
	if service := adminTokensRefreshServiceProvider(); service != nil {
		return service, nil
	}
	return nil, platform.NewAppError("Refresh service is not initialised", platform.ErrorKindServer, "refresh_service_not_initialised", 500, nil)
}
