package protocol

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestBuildConsolePayloadMatchesPythonFixtures(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "assistant", "content": "hi"},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "look"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://img"}},
			map[string]any{"type": "other", "text": "fallback"},
		}},
		{"role": "tool", "content": 123},
	}

	payload := BuildConsolePayload(ConsolePayloadOptions{Messages: messages, Model: "grok-4.3-high", Temperature: 0.2, TopP: 0.8, Stream: boolRef(false)})
	if payload["model"] != "grok-4.3" || payload["max_output_tokens"] != 1000000 || payload["tool_choice"] != "auto" {
		t.Fatalf("grok-4.3-high payload basics mismatch: %#v", payload)
	}
	if !reflect.DeepEqual(map[string]any{"effort": "high"}, payload["reasoning"]) {
		t.Fatalf("fixed high reasoning mismatch: %#v", payload["reasoning"])
	}
	if !reflect.DeepEqual([]any{"reasoning.encrypted_content"}, payload["include"]) || payload["stream"] != false {
		t.Fatalf("include/stream mismatch: %#v", payload)
	}

	input := payload["input"].([]map[string]any)
	wantThird := map[string]any{"role": "user", "content": []map[string]any{
		{"type": "input_text", "text": "look"},
		{"type": "input_image", "image_url": "https://img"},
		{"type": "input_text", "text": "fallback"},
	}}
	if !reflect.DeepEqual(wantThird, input[2]) {
		t.Fatalf("content block conversion mismatch\nwant: %#v\n got: %#v", wantThird, input[2])
	}

	payload = BuildConsolePayload(ConsolePayloadOptions{Messages: messages, Model: "grok-4.20-multi-agent-console", ReasoningEffort: "minimal", Temperature: 0.2, TopP: 0.8, Stream: boolRef(false)})
	if payload["model"] != "grok-4.20-multi-agent-0309" || payload["max_output_tokens"] != 2000000 {
		t.Fatalf("multi-agent model mapping mismatch: %#v", payload)
	}
	if !reflect.DeepEqual(map[string]any{"effort": "low"}, payload["reasoning"]) {
		t.Fatalf("multi-agent reasoning mismatch: %#v", payload["reasoning"])
	}

	payload = BuildConsolePayload(ConsolePayloadOptions{Messages: messages, Model: "custom-model", ReasoningEffort: "xhigh", Temperature: 0.2, TopP: 0.8, Stream: boolRef(false)})
	if _, ok := payload["reasoning"]; ok {
		t.Fatalf("custom model must not include reasoning: %#v", payload)
	}
	if _, ok := payload["tools"]; ok {
		t.Fatalf("custom model must not include tools: %#v", payload)
	}
}

func TestBuildConsolePayloadMatchesPythonContentFallbacks(t *testing.T) {
	messages := []map[string]any{
		{"role": "developer", "content": "dev"},
		{"role": "user", "content": []any{
			map[string]any{"type": "unknown", "text": "fallback"},
			map[string]any{"type": "unknown"},
			"skip",
			map[string]any{"type": "image_url", "image_url": map[string]any{}},
		}},
		{"role": "tool", "content": 123},
		{"content": nil},
	}
	payload := BuildConsolePayload(ConsolePayloadOptions{Messages: messages, Model: "grok-build-console"})
	if payload["model"] != "grok-build-0.1" || payload["max_output_tokens"] != 256000 {
		t.Fatalf("build model mapping mismatch: %#v", payload)
	}
	if _, ok := payload["reasoning"]; ok {
		t.Fatalf("grok-build payload must not include reasoning: %#v", payload)
	}
	if payload["tool_choice"] != "auto" {
		t.Fatalf("grok-build payload should include auto tool choice: %#v", payload)
	}
	input := payload["input"].([]map[string]any)
	want := []map[string]any{
		{"role": "system", "content": []map[string]any{{"type": "input_text", "text": "dev"}}},
		{"role": "user", "content": []map[string]any{
			{"type": "input_text", "text": "fallback"},
			{"type": "input_text", "text": "{'type': 'unknown'}"},
		}},
		{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "123"}}},
		{"role": "user", "content": []map[string]any{{"type": "input_text", "text": "None"}}},
	}
	if !reflect.DeepEqual(want, input) {
		t.Fatalf("content fallback mismatch\nwant: %#v\n got: %#v", want, input)
	}
}

func TestStreamConsoleChatMatchesPythonFeedbackAndLinePairing(t *testing.T) {
	proxy := &fakeConsoleProxy{}
	poster := &fakeConsolePoster{response: ConsoleStreamResponse{
		StatusCode: 200,
		Lines: []string{
			"event: response.output_text.delta",
			`data: {"delta":"hi"}`,
			"data: [DONE]",
		},
	}}
	events, err := StreamConsoleChat(context.Background(), "tok", map[string]any{"model": "x"}, ConsoleStreamOptions{Proxy: proxy, Poster: poster, TimeoutS: 120})
	if err != nil {
		t.Fatalf("StreamConsoleChat returned error: %v", err)
	}
	if !reflect.DeepEqual([]ConsoleStreamEvent{{EventType: "response.output_text.delta", Data: `{"delta":"hi"}`}}, events) {
		t.Fatalf("stream events mismatch: %#v", events)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackSuccess || *proxy.feedbacks[0].StatusCode != 200 {
		t.Fatalf("success feedback mismatch: %#v", proxy.feedbacks)
	}

	proxy = &fakeConsoleProxy{}
	poster = &fakeConsolePoster{response: ConsoleStreamResponse{StatusCode: 403, Body: "blocked"}}
	_, err = StreamConsoleChat(context.Background(), "tok", map[string]any{"model": "x"}, ConsoleStreamOptions{Proxy: proxy, Poster: poster})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 403 || upstream.Body != "blocked" {
		t.Fatalf("status error mismatch: %#v", err)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackChallenge || *proxy.feedbacks[0].StatusCode != 403 {
		t.Fatalf("status feedback mismatch: %#v", proxy.feedbacks)
	}

	proxy = &fakeConsoleProxy{}
	poster = &fakeConsolePoster{err: errors.New("dial failed")}
	_, err = StreamConsoleChat(context.Background(), "tok", map[string]any{"model": "x"}, ConsoleStreamOptions{Proxy: proxy, Poster: poster})
	if !errors.As(err, &upstream) || upstream.Status != 502 || upstream.Error() != "Console transport failed: dial failed" {
		t.Fatalf("transport error mismatch: %#v", err)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackTransportError {
		t.Fatalf("transport feedback mismatch: %#v", proxy.feedbacks)
	}

	proxy = &fakeConsoleProxy{}
	poster = &fakeConsolePoster{response: ConsoleStreamResponse{StatusCode: 500, Body: strings.Repeat("x", 500)}}
	_, err = StreamConsoleChat(context.Background(), "tok", map[string]any{"model": "x"}, ConsoleStreamOptions{Proxy: proxy, Poster: poster})
	if !errors.As(err, &upstream) || upstream.Status != 500 || len(upstream.Body) != 400 {
		t.Fatalf("5xx status truncation mismatch: %#v", err)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackUpstream5xx || *proxy.feedbacks[0].StatusCode != 500 {
		t.Fatalf("5xx feedback mismatch: %#v", proxy.feedbacks)
	}
}

func TestConsoleLineAdapterAndFeedbackMatchPythonFixtures(t *testing.T) {
	lineCases := []struct {
		line     string
		wantTyp  string
		wantData string
	}{
		{"", "skip", ""},
		{"event: response.output_text.delta", "event", "response.output_text.delta"},
		{`data: {"delta":"hi"}`, "data", `{"delta":"hi"}`},
		{"data: [DONE]", "done", ""},
		{`  data: {\"delta\":\"trim\"}  `, "data", `{\"delta\":\"trim\"}`},
		{"junk", "skip", ""},
	}
	for _, tc := range lineCases {
		gotTyp, gotData := ClassifyConsoleLine(tc.line)
		if gotTyp != tc.wantTyp || gotData != tc.wantData {
			t.Fatalf("ClassifyConsoleLine(%q)=(%q,%q), want (%q,%q)", tc.line, gotTyp, gotData, tc.wantTyp, tc.wantData)
		}
	}

	adapter := NewConsoleStreamAdapter()
	got, err := adapter.Feed("response.output_text.delta", `{"delta":"hel"}`)
	if err != nil || !reflect.DeepEqual([]string{"hel"}, got) {
		t.Fatalf("delta feed mismatch got=%#v err=%v", got, err)
	}
	got, err = adapter.Feed("response.output_text.delta", `{"delta":"lo"}`)
	if err != nil || !reflect.DeepEqual([]string{"lo"}, got) {
		t.Fatalf("second delta feed mismatch got=%#v err=%v", got, err)
	}
	got, err = adapter.Feed("response.completed", `{"response":{"usage":{"input_tokens":2,"output_tokens":3}}}`)
	if err != nil || len(got) != 0 || adapter.FullText() != "hello" {
		t.Fatalf("completed feed mismatch got=%#v err=%v text=%q", got, err, adapter.FullText())
	}
	got, err = adapter.Feed("response.output_text.delta", `{"delta":"ignored"}`)
	if err != nil || len(got) != 0 {
		t.Fatalf("done adapter should ignore later deltas got=%#v err=%v", got, err)
	}
	if !reflect.DeepEqual(map[string]any{"input_tokens": float64(2), "output_tokens": float64(3)}, adapter.Usage) {
		t.Fatalf("usage mismatch: %#v", adapter.Usage)
	}

	_, err = NewConsoleStreamAdapter().Feed("error", `{"message":"boom"}`)
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 502 || upstream.Error() != "Console API error: boom" {
		t.Fatalf("console error mismatch: %#v", err)
	}
	_, err = NewConsoleStreamAdapter().Feed("error", `{}`)
	if !errors.As(err, &upstream) || upstream.Status != 502 || upstream.Error() != "Console API error: map[]" {
		t.Fatalf("empty console error mismatch: %#v", err)
	}

	if fb := ConsoleStatusFeedback(403); fb.Kind != controlproxy.ProxyFeedbackChallenge || *fb.StatusCode != 403 {
		t.Fatalf("403 feedback mismatch: %#v", fb)
	}
	if fb := ConsoleStatusFeedback(429); fb.Kind != controlproxy.ProxyFeedbackRateLimited || *fb.StatusCode != 429 {
		t.Fatalf("429 feedback mismatch: %#v", fb)
	}
	if fb := ConsoleStatusFeedback(500); fb.Kind != controlproxy.ProxyFeedbackUpstream5xx || *fb.StatusCode != 500 {
		t.Fatalf("500 feedback mismatch: %#v", fb)
	}
	if fb := ConsoleStatusFeedback(418); fb.Kind != controlproxy.ProxyFeedbackForbidden || *fb.StatusCode != 418 {
		t.Fatalf("418 feedback mismatch: %#v", fb)
	}
	if fb := ConsoleSuccessFeedback(); fb.Kind != controlproxy.ProxyFeedbackSuccess || *fb.StatusCode != 200 {
		t.Fatalf("success feedback mismatch: %#v", fb)
	}
	if fb := ConsoleTransportErrorFeedback(); fb.Kind != controlproxy.ProxyFeedbackTransportError || fb.StatusCode != nil {
		t.Fatalf("transport feedback mismatch: %#v", fb)
	}
}

func boolRef(v bool) *bool {
	return &v
}

type fakeConsoleProxy struct {
	feedbacks []controlproxy.ProxyFeedback
}

func (f *fakeConsoleProxy) Acquire(context.Context) (controlproxy.ProxyLease, error) {
	return controlproxy.NewProxyLease("console-lease"), nil
}

func (f *fakeConsoleProxy) Feedback(_ context.Context, _ controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	f.feedbacks = append(f.feedbacks, feedback)
	return nil
}

type fakeConsolePoster struct {
	response ConsoleStreamResponse
	err      error
}

func (f *fakeConsolePoster) PostConsoleStream(context.Context, ConsoleStreamRequest) (ConsoleStreamResponse, error) {
	return f.response, f.err
}
