package openai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	"github.com/jiujiu532/grok2api/app/platform"
)

func TestConsoleResponsesNonStreamBuildsResponseObject(t *testing.T) {
	resetChatDepsForTest(t)
	stream := false
	emitThink := false
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	refresh := &fakeChatRefreshService{}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatRefreshService = func() chatRefreshProvider { return refresh }
	chatTimeoutSeconds = func() float64 { return 66.5 }
	consoleStreamChat = func(_ context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
		reasoning, _ := payload["reasoning"].(map[string]any)
		if token != "tok1" || payload["model"] != "grok-4.3" || payload["stream"] != true || reasoning["effort"] != "none" {
			t.Fatalf("token/payload=%q/%#v", token, payload)
		}
		if timeoutS != 66.5 {
			t.Fatalf("timeout=%v want 66.5", timeoutS)
		}
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"hello"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":4,"output_tokens":5}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:      "grok-4.3-console",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Stream:     &stream,
		EmitThink:  &emitThink,
		ResponseID: "resp_test",
		MessageID:  "msg_test",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses err=%v", err)
	}
	if result.IsStream {
		t.Fatalf("expected non-stream result: %#v", result)
	}
	if result.Response["id"] != "resp_test" || result.Response["status"] != "completed" {
		t.Fatalf("response=%#v", result.Response)
	}
	output := result.Response["output"].([]map[string]any)
	item := output[0]
	content := item["content"].([]map[string]any)
	if item["id"] != "msg_test" || item["type"] != "message" || content[0]["text"] != "hello" {
		t.Fatalf("output=%#v", output)
	}
	usage := result.Response["usage"].(map[string]any)
	if usage["input_tokens"] != 4 || usage["output_tokens"] != 5 || usage["total_tokens"] != 9 {
		t.Fatalf("usage=%#v", usage)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 1 || dir.feedbacks[0].Kind != feedbackKindSuccess {
		t.Fatalf("dir=%#v releases=%d", dir.feedbacks, dir.releases)
	}
	if refresh.refreshCalls != 1 || refresh.token != "tok1" || refresh.modeID != int(model.ModeConsole) {
		t.Fatalf("refresh=%#v", refresh)
	}
}

func TestConsoleResponsesStreamFramesResponsesEvents(t *testing.T) {
	resetChatDepsForTest(t)
	stream := true
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	consoleStreamChat = func(context.Context, string, map[string]any, float64) ([]protocol.ConsoleStreamEvent, error) {
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"he"}`},
			{EventType: "response.output_text.delta", Data: `{"delta":"llo"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":2,"output_tokens":3}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:      "grok-4.3-console",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Stream:     &stream,
		ResponseID: "resp_stream",
		MessageID:  "msg_stream",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses stream err=%v", err)
	}
	if !result.IsStream {
		t.Fatalf("expected stream result: %#v", result)
	}
	joined := strings.Join(result.StreamFrames, "")
	for _, want := range []string{
		"event: response.created",
		"event: response.in_progress",
		"event: response.output_item.added",
		"event: response.content_part.added",
		"event: response.output_text.delta",
		`"delta":"he"`,
		`"delta":"llo"`,
		"event: response.output_text.done",
		`"text":"hello"`,
		"event: response.content_part.done",
		"event: response.output_item.done",
		"event: response.completed",
		`"input_tokens":2`,
		`"output_tokens":3`,
		"data: [DONE]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("stream frames missing %q:\n%s", want, joined)
		}
	}
}

func TestConsoleResponsesRetriesRetryableUpstreamStatus(t *testing.T) {
	resetChatDepsForTest(t)
	stream := false
	dir := &fakeChatDirectory{accounts: []chatAccount{
		{Token: "tokA", ModeID: model.ModeConsole},
		{Token: "tokB", ModeID: model.ModeConsole},
	}}
	chatSelectionMaxRetries = func() int { return 1 }
	chatRetryConfig = func() map[string]any { return map[string]any{"retry.on_codes": "429"} }
	chatDirectoryProvider = func() chatDirectory { return dir }
	calls := []string{}
	consoleStreamChat = func(_ context.Context, token string, _ map[string]any, _ float64) ([]protocol.ConsoleStreamEvent, error) {
		calls = append(calls, token)
		if token == "tokA" {
			return nil, platform.NewUpstreamError("rate limited", 429, "")
		}
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"ok"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":1,"output_tokens":1}}}`},
		}, nil
	}

	result, err := ConsoleResponses(context.Background(), consoleResponseOptions{
		Model:      "grok-4.3-console",
		Messages:   []map[string]any{{"role": "user", "content": "hi"}},
		Stream:     &stream,
		ResponseID: "resp_retry",
		MessageID:  "msg_retry",
	})
	if err != nil {
		t.Fatalf("ConsoleResponses retry err=%v", err)
	}
	if result.Response["status"] != "completed" {
		t.Fatalf("response=%#v", result.Response)
	}
	if !reflect.DeepEqual(calls, []string{"tokA", "tokB"}) {
		t.Fatalf("calls=%#v", calls)
	}
	if !reflect.DeepEqual(dir.excludes, [][]string{{}, {"tokA"}}) {
		t.Fatalf("excludes=%#v", dir.excludes)
	}
	if len(dir.feedbacks) != 2 || dir.feedbacks[0].Kind != feedbackKindRateLimited || dir.feedbacks[1].Kind != feedbackKindSuccess {
		t.Fatalf("feedbacks=%#v", dir.feedbacks)
	}
	if dir.releases != 2 {
		t.Fatalf("releases=%d", dir.releases)
	}
}
