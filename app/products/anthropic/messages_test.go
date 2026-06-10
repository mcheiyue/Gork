package anthropic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
	"github.com/jiujiu532/grok2api/app/control/model"
	dataaccount "github.com/jiujiu532/grok2api/app/dataplane/account"
	"github.com/jiujiu532/grok2api/app/platform"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

func TestMessagesNonStreamBuildsAnthropicMessage(t *testing.T) {
	resetMessagesDepsForTest(t)
	directory := &fakeMessagesDirectory{accounts: []messagesAccount{{Token: "tok1", ModeID: model.ModeAuto}}}
	messagesDirectoryProvider = func() messagesDirectory { return directory }
	messagesStream = func(_ context.Context, options messagesStreamOptions) ([]string, error) {
		if options.Token != "tok1" || !strings.Contains(options.Message, "[user]: hi") {
			t.Fatalf("stream options=%#v", options)
		}
		return []string{
			`data: {"result":{"response":{"token":"hello ","isThinking":false,"messageTag":"final"}}}`,
			`data: {"result":{"response":{"token":"world","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}, nil
	}

	result, err := Messages(context.Background(), MessagesOptions{
		Model:     "grok-4.20-auto",
		Messages:  []map[string]any{{"role": "user", "content": "hi"}},
		Stream:    false,
		MessageID: "msg_test",
	})
	if err != nil {
		t.Fatalf("Messages err=%v", err)
	}
	if result.IsStream {
		t.Fatalf("unexpected stream result")
	}
	body := result.Response
	content := body["content"].([]map[string]any)
	if body["id"] != "msg_test" || content[0]["text"] != "hello world" {
		t.Fatalf("body=%#v", body)
	}
	if len(directory.released) != 1 || len(directory.feedback) != 1 || directory.feedback[0].Kind != messagesFeedbackSuccess {
		t.Fatalf("directory released=%#v feedback=%#v", directory.released, directory.feedback)
	}
}

func TestMessagesStreamBuildsAnthropicSSE(t *testing.T) {
	resetMessagesDepsForTest(t)
	directory := &fakeMessagesDirectory{accounts: []messagesAccount{{Token: "tok1", ModeID: model.ModeAuto}}}
	messagesDirectoryProvider = func() messagesDirectory { return directory }
	messagesStream = func(context.Context, messagesStreamOptions) ([]string, error) {
		return []string{
			`data: {"result":{"response":{"token":"thinking","isThinking":true,"messageTag":"thought"}}}`,
			`data: {"result":{"response":{"token":"answer","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}, nil
	}

	result, err := Messages(context.Background(), MessagesOptions{
		Model:     "grok-4.20-auto",
		Messages:  []map[string]any{{"role": "user", "content": "hi"}},
		Stream:    true,
		EmitThink: true,
		MessageID: "msg_stream",
	})
	if err != nil {
		t.Fatalf("Messages stream err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	for _, want := range []string{
		"event: message_start",
		`"id":"msg_stream"`,
		"event: ping",
		`"thinking_delta"`,
		`"text_delta"`,
		`"text":"answer"`,
		"event: message_delta",
		"event: message_stop",
		"data: [DONE]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream missing %q in %s", want, joined)
		}
	}
}

func TestMessagesStreamConvertsToolCallToAnthropicToolUse(t *testing.T) {
	resetMessagesDepsForTest(t)
	directory := &fakeMessagesDirectory{accounts: []messagesAccount{{Token: "tok1", ModeID: model.ModeAuto}}}
	messagesDirectoryProvider = func() messagesDirectory { return directory }
	messagesStream = func(context.Context, messagesStreamOptions) ([]string, error) {
		return []string{
			`data: {"result":{"response":{"token":"<tool_calls><tool_call><tool_name>lookup</tool_name><parameters>{\"q\":\"go\"}</parameters></tool_call></tool_calls>","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}, nil
	}

	result, err := Messages(context.Background(), MessagesOptions{
		Model:     "grok-4.20-auto",
		Messages:  []map[string]any{{"role": "user", "content": "hi"}},
		Stream:    true,
		Tools:     []map[string]any{{"name": "lookup", "input_schema": map[string]any{"type": "object"}}},
		MessageID: "msg_tool",
	})
	if err != nil {
		t.Fatalf("Messages tool stream err=%v", err)
	}
	joined := strings.Join(result.StreamFrames, "")
	if strings.Contains(joined, "<tool_calls>") {
		t.Fatalf("raw tool XML leaked: %s", joined)
	}
	for _, want := range []string{`"type":"tool_use"`, `"name":"lookup"`, `"input_json_delta"`, `"stop_reason":"tool_use"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tool stream missing %q in %s", want, joined)
		}
	}
}

func TestMessagesRetriesConfiguredStatusAndExcludesToken(t *testing.T) {
	resetMessagesDepsForTest(t)
	directory := &fakeMessagesDirectory{accounts: []messagesAccount{
		{Token: "tok-first", ModeID: model.ModeAuto},
		{Token: "tok-second", ModeID: model.ModeAuto},
	}}
	messagesDirectoryProvider = func() messagesDirectory { return directory }
	messagesMaxRetries = func() int { return 1 }
	messagesRetryCodes = func() map[int]struct{} { return map[int]struct{}{418: {}} }
	calls := 0
	messagesStream = func(_ context.Context, options messagesStreamOptions) ([]string, error) {
		calls++
		if calls == 1 {
			return nil, platform.NewUpstreamError("retry me", 418, "")
		}
		if options.Token != "tok-second" {
			t.Fatalf("second attempt token = %q", options.Token)
		}
		return []string{
			`data: {"result":{"response":{"token":"ok","isThinking":false,"messageTag":"final"}}}`,
			`data: [DONE]`,
		}, nil
	}

	_, err := Messages(context.Background(), MessagesOptions{
		Model:    "grok-4.20-auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatalf("Messages err=%v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if len(directory.excluded) < 2 || len(directory.excluded[1]) != 1 || directory.excluded[1][0] != "tok-first" {
		t.Fatalf("excluded=%#v", directory.excluded)
	}
}

func TestMessagesUsesConfiguredTimeoutAndFeedbackMapping(t *testing.T) {
	resetMessagesDepsForTest(t)
	directory := &fakeMessagesDirectory{accounts: []messagesAccount{{Token: "tok", ModeID: model.ModeAuto}}}
	messagesDirectoryProvider = func() messagesDirectory { return directory }
	messagesTimeoutSeconds = func() float64 { return 33.5 }
	seenTimeout := 0.0
	messagesStream = func(_ context.Context, options messagesStreamOptions) ([]string, error) {
		seenTimeout = options.TimeoutSeconds
		return nil, platform.NewUpstreamError("forbidden", 403, "token expired")
	}

	_, err := Messages(context.Background(), MessagesOptions{
		Model:    "grok-4.20-auto",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatalf("expected upstream error")
	}
	if seenTimeout != 33.5 {
		t.Fatalf("timeout = %v", seenTimeout)
	}
	if len(directory.feedback) != 1 || directory.feedback[0].Kind != messagesFeedbackUnauthorized {
		t.Fatalf("feedback=%#v", directory.feedback)
	}
}

func TestMessagesProductionDefaultsUseConfigAndRefreshRuntime(t *testing.T) {
	oldConfig := platformconfig.GlobalConfig
	oldStrategy := dataaccount.CurrentStrategy()
	oldRefresh := controlaccount.GetRefreshService()
	t.Cleanup(func() {
		platformconfig.GlobalConfig = oldConfig
		_ = dataaccount.SetStrategy(oldStrategy)
		controlaccount.SetRefreshService(oldRefresh)
	})
	defaults := filepath.Join(t.TempDir(), "config.defaults.toml")
	if err := os.WriteFile(defaults, []byte("[chat]\ntimeout = 120.0\n\n[retry]\non_codes = \"429,401,503\"\n"), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	platformconfig.GlobalConfig = platformconfig.NewConfigSnapshot(consoleMessagesConfigBackend{data: map[string]any{
		"chat":  map[string]any{"timeout": 44.5},
		"retry": map[string]any{"on_codes": []any{"418", 429}},
	}}, platformconfig.ConfigSnapshotOptions{})
	if err := platformconfig.GlobalConfig.Load(context.Background(), defaults); err != nil {
		t.Fatalf("load config: %v", err)
	}

	if got := messagesTimeoutSeconds(); got != 44.5 {
		t.Fatalf("timeout = %v", got)
	}
	retryCodes := messagesRetryCodes()
	if _, ok := retryCodes[418]; !ok {
		t.Fatalf("retry codes missing 418: %#v", retryCodes)
	}

	refresh := &fakeConsoleMessagesRefreshService{}
	controlaccount.SetRefreshService(refresh)
	if err := dataaccount.SetStrategy("random"); err != nil {
		t.Fatalf("set random strategy: %v", err)
	}
	messagesQuotaSync(context.Background(), "tok", 5)
	if refresh.refreshCalls != 0 {
		t.Fatalf("random strategy quota sync calls = %d", refresh.refreshCalls)
	}
	if err := dataaccount.SetStrategy("quota"); err != nil {
		t.Fatalf("set quota strategy: %v", err)
	}
	messagesQuotaSync(context.Background(), "tok", 5)
	messagesFailSync(context.Background(), "tok", 5, errors.New("boom"))
	if refresh.refreshCalls != 1 || refresh.failureCalls != 1 {
		t.Fatalf("refreshCalls=%d failureCalls=%d", refresh.refreshCalls, refresh.failureCalls)
	}
}

func resetMessagesDepsForTest(t *testing.T) {
	t.Helper()
	oldDirectory := messagesDirectoryProvider
	oldStream := messagesStream
	oldMaxRetries := messagesMaxRetries
	oldRetryCodes := messagesRetryCodes
	oldTimeout := messagesTimeoutSeconds
	oldQuotaSync := messagesQuotaSync
	oldFailSync := messagesFailSync
	oldImageResolver := messagesImageResolver
	messagesDirectoryProvider = func() messagesDirectory { return nil }
	messagesStream = func(context.Context, messagesStreamOptions) ([]string, error) {
		return nil, errMessagesStreamNotConfigured
	}
	messagesMaxRetries = func() int { return 0 }
	messagesRetryCodes = func() map[int]struct{} { return map[int]struct{}{429: {}, 401: {}, 503: {}} }
	messagesTimeoutSeconds = func() float64 { return 120.0 }
	messagesQuotaSync = func(context.Context, string, int) {}
	messagesFailSync = func(context.Context, string, int, error) {}
	messagesImageResolver = func(_ context.Context, _ string, rawURL string, _ string) (string, error) { return rawURL, nil }
	t.Cleanup(func() {
		messagesDirectoryProvider = oldDirectory
		messagesStream = oldStream
		messagesMaxRetries = oldMaxRetries
		messagesRetryCodes = oldRetryCodes
		messagesTimeoutSeconds = oldTimeout
		messagesQuotaSync = oldQuotaSync
		messagesFailSync = oldFailSync
		messagesImageResolver = oldImageResolver
	})
}

type fakeMessagesDirectory struct {
	accounts []messagesAccount
	released []messagesAccount
	feedback []messagesFeedback
	excluded [][]string
}

func (d *fakeMessagesDirectory) ReserveMessagesAccount(_ context.Context, _ model.ModelSpec, excluded []string) (messagesAccount, bool, error) {
	d.excluded = append(d.excluded, append([]string(nil), excluded...))
	if len(d.accounts) == 0 {
		return messagesAccount{}, false, nil
	}
	account := d.accounts[0]
	d.accounts = d.accounts[1:]
	return account, true, nil
}

func (d *fakeMessagesDirectory) ReleaseMessagesAccount(_ context.Context, account messagesAccount) error {
	d.released = append(d.released, account)
	return nil
}

func (d *fakeMessagesDirectory) FeedbackMessagesAccount(_ context.Context, feedback messagesFeedback) error {
	d.feedback = append(d.feedback, feedback)
	return nil
}
