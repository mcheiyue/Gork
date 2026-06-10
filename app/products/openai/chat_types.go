package openai

import (
	"context"
	"github.com/jiujiu532/grok2api/app/control/model"
	dataaccount "github.com/jiujiu532/grok2api/app/dataplane/account"
)

type accountFeedbackKind string

const (
	feedbackKindSuccess      accountFeedbackKind = "success"
	feedbackKindUnauthorized accountFeedbackKind = "unauthorized"
	feedbackKindForbidden    accountFeedbackKind = "forbidden"
	feedbackKindRateLimited  accountFeedbackKind = "rate_limited"
	feedbackKindServerError  accountFeedbackKind = "server_error"
)

type chatAccount struct {
	Token  string
	ModeID model.ModeID
	lease  dataaccount.AccountLease
}

type chatFeedback struct {
	Token  string
	Kind   accountFeedbackKind
	ModeID model.ModeID
}

type chatDirectory interface {
	ReserveChatAccount(context.Context, model.ModelSpec, []string) (chatAccount, bool, error)
	ReleaseChatAccount(context.Context, chatAccount) error
	FeedbackChatAccount(context.Context, chatFeedback) error
}

type chatRefreshProvider interface {
	RefreshCall(context.Context, string, int) error
	RecordFailure(context.Context, string, int, error) error
	RefreshOnDemand(context.Context) (chatRefreshResult, error)
}

type chatRefreshResult struct {
	Refreshed   int
	Failed      int
	RateLimited int
}

type chatStreamOptions struct {
	Token               string
	ModeID              model.ModeID
	Message             string
	Files               []string
	ToolOverrides       map[string]any
	ModelConfigOverride map[string]any
	RequestOverrides    map[string]any
	TimeoutSeconds      float64
}

type chatStreamRequest struct {
	Token          string
	Headers        map[string]string
	PayloadBytes   []byte
	TimeoutSeconds float64
}

type chatStreamResponse struct {
	StatusCode int
	Body       string
	Lines      []string
}

type chatCompletionState struct {
	Text          string
	Thinking      string
	ImageTexts    []string
	References    string
	Annotations   []map[string]any
	SearchSources []map[string]any
}

type chatResponseBuildOptions struct {
	Model      string
	Message    any
	ResponseID string
	ToolNames  []string
	EmitThink  bool
	State      chatCompletionState
}

type chatCompletionOptions struct {
	Model            string
	Messages         []map[string]any
	Stream           *bool
	EmitThink        *bool
	Tools            []map[string]any
	ToolChoice       any
	Temperature      float64
	TopP             float64
	RequestOverrides map[string]any
}

type chatCompletionPlan struct {
	Spec             model.ModelSpec
	IsStream         bool
	EmitThink        bool
	IsConsole        bool
	Message          string
	Files            []string
	MaxRetries       int
	RetryCodes       map[int]struct{}
	ResponseID       string
	TimeoutSeconds   float64
	ToolNames        []string
	ToolOverrides    map[string]any
	RequestOverrides map[string]any
}

type chatCompletionResult struct {
	IsStream     bool
	StreamFrames []string
	Response     map[string]any
}
