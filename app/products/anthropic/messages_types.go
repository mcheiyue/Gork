package anthropic

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
	"github.com/jiujiu532/grok2api/app/control/model"
	dataaccount "github.com/jiujiu532/grok2api/app/dataplane/account"
	"github.com/jiujiu532/grok2api/app/platform"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
	"github.com/jiujiu532/grok2api/app/products"
)

type messagesFeedbackKind string

const (
	messagesFeedbackSuccess      messagesFeedbackKind = "success"
	messagesFeedbackUnauthorized messagesFeedbackKind = "unauthorized"
	messagesFeedbackForbidden    messagesFeedbackKind = "forbidden"
	messagesFeedbackRateLimited  messagesFeedbackKind = "rate_limited"
	messagesFeedbackServerError  messagesFeedbackKind = "server_error"
)

var errMessagesStreamNotConfigured = platform.NewUpstreamError("messages stream is not configured", 502, "")

type MessagesOptions struct {
	Model       string
	Messages    []map[string]any
	System      any
	Stream      bool
	EmitThink   bool
	Temperature float64
	TopP        float64
	Tools       []map[string]any
	ToolChoice  any
	MessageID   string
}

type MessagesResult struct {
	IsStream     bool
	StreamFrames []string
	Response     map[string]any
}

type messagesAccount struct {
	Token  string
	ModeID model.ModeID
	lease  dataaccount.AccountLease
}

type messagesFeedback struct {
	Token  string
	Kind   messagesFeedbackKind
	ModeID model.ModeID
}

type messagesDirectory interface {
	ReserveMessagesAccount(context.Context, model.ModelSpec, []string) (messagesAccount, bool, error)
	ReleaseMessagesAccount(context.Context, messagesAccount) error
	FeedbackMessagesAccount(context.Context, messagesFeedback) error
}

type messagesStreamOptions struct {
	Token          string
	ModeID         model.ModeID
	Message        string
	Files          []string
	TimeoutSeconds float64
}

type messagesPlan struct {
	Spec           model.ModelSpec
	IsStream       bool
	EmitThink      bool
	Message        string
	Files          []string
	ToolNames      []string
	MessageID      string
	MaxRetries     int
	RetryCodes     map[int]struct{}
	TimeoutSeconds float64
	Internal       []map[string]any
}

var (
	messagesDirectoryProvider = defaultMessagesDirectoryProvider
	messagesStream            = defaultMessagesStream
	messagesMaxRetries        = products.SelectionMaxRetries
	messagesTimeoutSeconds    = defaultMessagesTimeoutSeconds
	messagesRetryCodes        = defaultMessagesRetryCodes
	messagesQuotaSync         = defaultMessagesQuotaSync
	messagesFailSync          = defaultMessagesFailSync
	messagesImageResolver     = func(_ context.Context, _ string, rawURL string, _ string) (string, error) { return rawURL, nil }
)

type messagesDataDirectory struct {
	directory *dataaccount.AccountDirectory
}

type messagesReserveDirectory struct {
	directory *dataaccount.AccountDirectory
}

type messagesRefreshProvider interface {
	RefreshCallAsync(context.Context, string, int) error
	RecordFailureAsync(context.Context, string, int, error) error
}

func defaultMessagesDirectoryProvider() messagesDirectory {
	directory, err := dataaccount.GetAccountDirectory(context.Background(), nil)
	if err != nil || directory == nil {
		return nil
	}
	return messagesDataDirectory{directory: directory}
}

func (d messagesDataDirectory) ReserveMessagesAccount(ctx context.Context, spec model.ModelSpec, excluded []string) (messagesAccount, bool, error) {
	nowS := appruntime.NowS()
	lease, selectedMode, ok, err := products.ReserveAccount(ctx, messagesReserveDirectory{directory: d.directory}, spec, products.ReserveAccountOptions{
		ExcludeTokens: excluded,
		NowSOverride:  &nowS,
	})
	if err != nil || !ok {
		return messagesAccount{}, false, err
	}
	accountLease, ok := lease.(dataaccount.AccountLease)
	if !ok {
		return messagesAccount{}, false, fmt.Errorf("unexpected account lease type %T", lease)
	}
	return messagesAccount{Token: accountLease.Token, ModeID: selectedMode, lease: accountLease}, true, nil
}

func (d messagesDataDirectory) ReleaseMessagesAccount(_ context.Context, account messagesAccount) error {
	d.directory.Release(account.lease)
	return nil
}

func (d messagesDataDirectory) FeedbackMessagesAccount(_ context.Context, feedback messagesFeedback) error {
	d.directory.Feedback(feedback.Token, controlaccount.FeedbackKind(feedback.Kind), int(feedback.ModeID), dataaccount.FeedbackOptions{NowS: intPtr(int(appruntime.NowS()))})
	return nil
}

func (d messagesReserveDirectory) Reserve(_ context.Context, query products.ReserveAccountQuery) (any, error) {
	lease, ok := d.directory.Reserve(query.PoolCandidates, int(query.ModeID), dataaccount.ReserveOptions{
		ExcludeTokens: query.ExcludeTokens,
		NowS:          int64PtrToIntPtr(query.NowSOverride),
	})
	if !ok {
		return nil, nil
	}
	return lease, nil
}

func defaultMessagesRetryCodes() map[int]struct{} {
	raw := platformconfig.GlobalConfig.Get("retry.on_codes", nil)
	if raw == nil {
		raw = platformconfig.GlobalConfig.Get("retry.retry_status_codes", "429,401,503")
	}
	return parseMessagesRetryCodes(raw)
}

func defaultMessagesTimeoutSeconds() float64 {
	return platformconfig.GlobalConfig.GetFloat("chat.timeout", 120.0)
}

func parseMessagesRetryCodes(value any) map[int]struct{} {
	result := map[int]struct{}{}
	var parts []any
	switch typed := value.(type) {
	case string:
		for _, part := range strings.Split(typed, ",") {
			parts = append(parts, strings.TrimSpace(part))
		}
	case []any:
		parts = append(parts, typed...)
	case []string:
		for _, part := range typed {
			parts = append(parts, part)
		}
	case []int:
		for _, part := range typed {
			parts = append(parts, part)
		}
	default:
		return result
	}
	for _, part := range parts {
		text := strings.TrimSpace(fmt.Sprint(part))
		if text == "" {
			continue
		}
		code, err := strconv.Atoi(text)
		if err == nil {
			result[code] = struct{}{}
		}
	}
	return result
}

func defaultMessagesQuotaSync(ctx context.Context, token string, modeID int) {
	if dataaccount.CurrentStrategy() != "quota" {
		return
	}
	if service := messagesRefreshService(); service != nil {
		_ = service.RefreshCallAsync(ctx, token, modeID)
	}
}

func defaultMessagesFailSync(ctx context.Context, token string, modeID int, err error) {
	if service := messagesRefreshService(); service != nil {
		_ = service.RecordFailureAsync(ctx, token, modeID, err)
	}
}

func messagesRefreshService() messagesRefreshProvider {
	service := controlaccount.GetRefreshService()
	if service == nil {
		return nil
	}
	provider, ok := any(service).(messagesRefreshProvider)
	if !ok {
		return nil
	}
	return provider
}
