package openai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/control/model"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

func TestConsoleReasoningEffortFromEmitThink(t *testing.T) {
	if got := consoleReasoningEffort(nil); got != "low" {
		t.Fatalf("nil effort=%q", got)
	}
	disabled := false
	if got := consoleReasoningEffort(&disabled); got != "none" {
		t.Fatalf("false effort=%q", got)
	}
	enabled := true
	if got := consoleReasoningEffort(&enabled); got != "low" {
		t.Fatalf("true effort=%q", got)
	}
}

func TestConsoleCompletionsNonStreamBuildsChatResponse(t *testing.T) {
	resetChatDepsForTest(t)
	stream := false
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	chatTimeoutSeconds = func() float64 { return 55.5 }
	consoleStreamChat = func(_ context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
		reasoning, _ := payload["reasoning"].(map[string]any)
		if token != "tok1" || payload["model"] != "grok-4.3" || reasoning["effort"] != "none" {
			t.Fatalf("token/payload=%q/%#v", token, payload)
		}
		if payload["stream"] != true {
			t.Fatalf("console upstream stream=%#v want true", payload["stream"])
		}
		if timeoutS != 55.5 {
			t.Fatalf("console timeout=%v want 55.5", timeoutS)
		}
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"hello"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":2,"output_tokens":3}}}`},
		}, nil
	}

	result, err := ConsoleCompletions(context.Background(), chatCompletionOptions{
		Model:     "grok-4.3-console",
		Messages:  []map[string]any{{"role": "user", "content": "hi"}},
		Stream:    &stream,
		EmitThink: &stream,
	})
	if err != nil {
		t.Fatalf("ConsoleCompletions err=%v", err)
	}
	choices := result.Response["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Fatalf("message=%#v", message)
	}
	usage := result.Response["usage"].(map[string]any)
	if usage["prompt_tokens"] != 2 || usage["completion_tokens"] != 3 {
		t.Fatalf("usage=%#v", usage)
	}
	if dir.releases != 1 || len(dir.feedbacks) != 1 || dir.feedbacks[0].Kind != feedbackKindSuccess {
		t.Fatalf("dir=%#v releases=%d", dir.feedbacks, dir.releases)
	}
}

func TestConsoleCompletionsStreamFrames(t *testing.T) {
	resetChatDepsForTest(t)
	stream := true
	dir := &fakeChatDirectory{accounts: []chatAccount{{Token: "tok1", ModeID: model.ModeConsole}}}
	chatDirectoryProvider = func() chatDirectory { return dir }
	consoleStreamChat = func(context.Context, string, map[string]any, float64) ([]protocol.ConsoleStreamEvent, error) {
		return []protocol.ConsoleStreamEvent{
			{EventType: "response.output_text.delta", Data: `{"delta":"hi"}`},
			{EventType: "response.completed", Data: `{"response":{"usage":{"input_tokens":1,"output_tokens":1}}}`},
		}, nil
	}

	result, err := ConsoleCompletions(context.Background(), chatCompletionOptions{
		Model:    "grok-4.3-console",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
		Stream:   &stream,
	})
	if err != nil {
		t.Fatalf("ConsoleCompletions stream err=%v", err)
	}
	if !result.IsStream || !reflect.DeepEqual(dir.excludes, [][]string{{}}) {
		t.Fatalf("result/excludes=%#v/%#v", result, dir.excludes)
	}
	joined := strings.Join(result.StreamFrames, "")
	if !strings.Contains(joined, `"content":"hi"`) || !strings.Contains(joined, `"finish_reason":"stop"`) || !strings.Contains(joined, "data: [DONE]") {
		t.Fatalf("frames=%s", joined)
	}
}
