package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/control/model"
	"github.com/dslzl/gork/app/dataplane/build"
	"github.com/dslzl/gork/app/platform"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

// 可注入依赖，便于单测；默认读 GlobalConfig + 可选 Build 账号目录。
var (
	buildFeatureEnabled = func() bool {
		return platformconfig.GlobalConfig.GetBool("features.build_provider", false)
	}
	buildCompletions = BuildCompletions
	buildAccountDir  = func() buildAccountDirectory { return defaultBuildAccountDirectory }
	buildAPIClient   = defaultBuildAPIClient
	buildOAuthClient = defaultBuildOAuthClient
)

// buildAccountDirectory 独立选号（不碰 SSO 池）。
type buildAccountDirectory interface {
	ListActive(ctx context.Context, now time.Time) ([]buildaccount.Account, error)
	UpdateTokens(ctx context.Context, id int64, access, refresh string, expiresAt time.Time) error
	SetStatus(ctx context.Context, id int64, status string, reason string) error
}

// defaultBuildAccountDirectory 由启动阶段注入；nil 表示未挂载。
var defaultBuildAccountDirectory buildAccountDirectory

// SetBuildAccountDirectory 挂载 Build 账号池（B-c 启动接线也可调用）。
func SetBuildAccountDirectory(dir buildAccountDirectory) {
	defaultBuildAccountDirectory = dir
}

type buildHTTPClient interface {
	CreateResponse(ctx context.Context, meta build.RequestMeta, body io.Reader) (*http.Response, error)
}

type buildTokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (build.TokenPayload, error)
}

func defaultBuildAPIClient() buildHTTPClient {
	cfg := build.ClientConfig{
		BaseURL:          platformconfig.GlobalConfig.GetStr("provider.build.base_url", build.DefaultBaseURL),
		ClientVersion:    platformconfig.GlobalConfig.GetStr("provider.build.client_version", build.DefaultClientVersion),
		ClientIdentifier: platformconfig.GlobalConfig.GetStr("provider.build.client_identifier", build.DefaultClientIDName),
		TokenAuth:        platformconfig.GlobalConfig.GetStr("provider.build.token_auth", build.DefaultTokenAuth),
		UserAgent:        platformconfig.GlobalConfig.GetStr("provider.build.user_agent", build.DefaultUserAgent),
		Timeout:          time.Duration(platformconfig.GlobalConfig.GetFloat("provider.build.timeout_seconds", 120)) * time.Second,
	}
	return build.NewAPIClient(nil, cfg)
}

func defaultBuildOAuthClient() buildTokenRefresher {
	cfg := build.OAuthConfig{
		ClientID:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_client_id", build.DefaultOAuthClientID),
		Scope:     platformconfig.GlobalConfig.GetStr("provider.build.oauth_scope", build.DefaultOAuthScope),
		DeviceURL: platformconfig.GlobalConfig.GetStr("provider.build.oauth_device_url", build.DefaultDeviceURL),
		TokenURL:  platformconfig.GlobalConfig.GetStr("provider.build.oauth_token_url", build.DefaultTokenURL),
	}
	return build.NewOAuthClient(nil, cfg)
}

// BuildCompletions 走 Build 上游 POST /responses，再转 OpenAI chat.completion。
// 支持非流式 JSON 与流式 SSE（上游 SSE → OpenAI chat.completion.chunk 帧）。
func BuildCompletions(ctx context.Context, options chatCompletionOptions) (chatCompletionResult, error) {
	if !buildFeatureEnabled() {
		return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
	}
	upstream := model.UpstreamIDFromBuildModel(options.Model)
	if upstream == "" {
		return chatCompletionResult{}, fmt.Errorf("Unknown model: '%s'", options.Model)
	}
	stream := false
	if options.Stream != nil {
		stream = *options.Stream
	}

	dir := buildAccountDir()
	if dir == nil {
		return chatCompletionResult{}, platform.NewRateLimitError("Build account directory not initialised")
	}
	accounts, err := dir.ListActive(ctx, time.Now().UTC())
	if err != nil {
		return chatCompletionResult{}, err
	}
	accounts = filterBuildAccountsByBilling(ctx, dir, accounts)
	if len(accounts) == 0 {
		return chatCompletionResult{}, platform.NewRateLimitError("No available Build accounts")
	}

	return runBuildCompletion(ctx, options, upstream, stream, accounts, dir, buildAPIClient(), buildOAuthClient())
}

// filterBuildAccountsByBilling 跳过已同步且额度耗尽的账号，并标记 cooling。
func filterBuildAccountsByBilling(ctx context.Context, dir buildAccountDirectory, accounts []buildaccount.Account) []buildaccount.Account {
	if len(accounts) == 0 {
		return accounts
	}
	out := make([]buildaccount.Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.Billing.QuotaExhausted() {
			if dir != nil {
				_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusCooling, "billing quota exhausted")
			}
			continue
		}
		out = append(out, acc)
	}
	return out
}

func finishBuildChat(modelName string, raw []byte) (chatCompletionResult, error) {
	response, err := build.ChatCompletionFromResponsesJSON(modelName, chatResponseID(), raw)
	if err != nil {
		return chatCompletionResult{}, err
	}
	return chatCompletionResult{Response: response}, nil
}

func ensureBuildAccessToken(
	ctx context.Context,
	dir buildAccountDirectory,
	oauth buildTokenRefresher,
	acc buildaccount.Account,
) (string, error) {
	if acc.AccessToken != "" && !acc.NeedsRefresh(time.Now().UTC(), 2*time.Minute) {
		return acc.AccessToken, nil
	}
	if acc.RefreshToken == "" {
		if acc.AccessToken != "" {
			return acc.AccessToken, nil
		}
		return "", fmt.Errorf("build account %d has no tokens", acc.ID)
	}
	tok, err := oauth.Refresh(ctx, acc.RefreshToken)
	if err != nil {
		if build.IsPermanentRefresh(err) {
			_ = dir.SetStatus(ctx, acc.ID, buildaccount.StatusExpired, "refresh permanent failure")
		}
		return "", err
	}
	refresh := firstNonEmptyStr(tok.RefreshToken, acc.RefreshToken)
	_ = dir.UpdateTokens(ctx, acc.ID, tok.AccessToken, refresh, tok.ExpiresAt)
	return tok.AccessToken, nil
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
