package protocol

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestBuildChatPayloadMatchesPythonFixture(t *testing.T) {
	payload := BuildChatPayload(ChatPayloadOptions{
		Message:             "hello",
		ModeID:              controlmodel.ModeFast,
		FileAttachments:     []string{"f1"},
		ToolOverrides:       map[string]any{"webSearch": true},
		ModelConfigOverride: map[string]any{"temperature": 0.1},
		RequestOverrides:    map[string]any{"disableSearch": true, "ignored": nil},
	})

	want := map[string]any{
		"message":               "hello",
		"modeId":                "fast",
		"fileAttachments":       []string{"f1"},
		"toolOverrides":         map[string]any{"webSearch": true},
		"responseMetadata":      map[string]any{"modelConfigOverride": map[string]any{"temperature": 0.1}},
		"disableMemory":         true,
		"temporary":             true,
		"disableSearch":         true,
		"enableImageGeneration": true,
		"imageGenerationCount":  2,
		"sendFinalMetadata":     true,
	}
	for key, expected := range want {
		if !reflect.DeepEqual(expected, payload[key]) {
			t.Fatalf("payload[%s] mismatch\nwant: %#v\n got: %#v", key, expected, payload[key])
		}
	}
	if _, ok := payload["ignored"]; ok {
		t.Fatalf("nil request override should be omitted")
	}
}

func TestBuildChatPayloadMatchesPythonDefaultsAndCustomOptions(t *testing.T) {
	temporary := false
	payload := BuildChatPayload(ChatPayloadOptions{
		Message:           "hi",
		ModeID:            controlmodel.ModeExpert,
		MemoryEnabled:     true,
		Temporary:         &temporary,
		CustomInstruction: " be kind ",
		RequestOverrides:  map[string]any{"imageGenerationCount": 3, "drop": nil},
	})

	wantToolOverrides := map[string]any{
		"gmailSearch":           false,
		"googleCalendarSearch":  false,
		"outlookSearch":         false,
		"outlookCalendarSearch": false,
		"googleDriveSearch":     false,
	}
	want := map[string]any{
		"message":               "hi",
		"modeId":                "expert",
		"disableMemory":         false,
		"temporary":             false,
		"customPersonality":     "be kind",
		"imageGenerationCount":  3,
		"toolOverrides":         wantToolOverrides,
		"enableImageGeneration": true,
		"sendFinalMetadata":     true,
	}
	for key, expected := range want {
		if !reflect.DeepEqual(expected, payload[key]) {
			t.Fatalf("payload[%s] mismatch\nwant: %#v\n got: %#v", key, expected, payload[key])
		}
	}
	if _, ok := payload["drop"]; ok {
		t.Fatalf("nil request override should be omitted")
	}
}

func TestClassifyLineMatchesPythonFixture(t *testing.T) {
	cases := []struct {
		line     string
		wantTyp  string
		wantData string
	}{
		{"", "skip", ""},
		{"data: [DONE]", "done", ""},
		{`data: {"a":1}`, "data", `{"a":1}`},
		{"event: ping", "skip", ""},
		{`{"raw":true}`, "data", `{"raw":true}`},
		{"junk", "skip", ""},
	}
	for _, tc := range cases {
		gotTyp, gotData := ClassifyLine(tc.line)
		if gotTyp != tc.wantTyp || gotData != tc.wantData {
			t.Fatalf("ClassifyLine(%q)=(%q,%q), want (%q,%q)", tc.line, gotTyp, gotData, tc.wantTyp, tc.wantData)
		}
	}
}

func TestStreamErrorFromPayloadMatchesPythonFixture(t *testing.T) {
	err := StreamErrorFromPayload(map[string]any{"error": map[string]any{"message": "too many requests", "code": float64(8)}})
	if err == nil || err.Status != 429 || err.Error() != "Upstream stream error: too many requests" || err.Body != `{"error":{"message":"too many requests","code":8}}` {
		t.Fatalf("rate-limit stream error mismatch: %#v", err)
	}

	err = StreamErrorFromPayload(map[string]any{"error": map[string]any{"error": "boom"}})
	if err == nil || err.Status != 502 || err.Error() != "Upstream stream error: boom" || err.Body != `{"error":{"error":"boom"}}` {
		t.Fatalf("generic stream error mismatch: %#v", err)
	}

	err = StreamErrorFromPayload(map[string]any{"error": map[string]any{"message": strings.Repeat("x", 500)}})
	if err == nil || err.Status != 502 || len(err.Body) != 400 {
		t.Fatalf("long stream error body should be truncated to 400 chars, got %#v", err)
	}

	if err = StreamErrorFromPayload(map[string]any{"error": "bad"}); err != nil {
		t.Fatalf("non-object stream error should be ignored, got %#v", err)
	}

	if streamErr := RaiseForStreamError(`not json`); streamErr != nil {
		t.Fatalf("invalid JSON stream frame should be ignored, got %#v", streamErr)
	}
	streamErr := RaiseForStreamError(`{"error":{"message":"rate limit"}}`)
	if streamErr == nil {
		t.Fatalf("stream error JSON should return upstream error")
	}

	_, feedErr := NewStreamAdapter(StreamAdapterOptions{}).Feed(`{"error":{"message":"rate limit"}}`)
	var upstream *platform.UpstreamError
	if !errors.As(feedErr, &upstream) || upstream.Status != 429 {
		t.Fatalf("Feed should raise upstream rate-limit error, got %#v", feedErr)
	}
}

func TestStreamAdapterEmitsTextImageAndCitationFixtures(t *testing.T) {
	adapter := NewStreamAdapter(StreamAdapterOptions{})
	events := mustFeed(t, adapter, `{"result":{"response":{"token":"hello ","isThinking":false,"messageTag":"final"}}}`)
	events = append(events, mustFeed(t, adapter, `{"result":{"response":{"token":"world","isThinking":false,"messageTag":"final"}}}`)...)
	events = append(events, mustFeed(t, adapter, `{"result":{"response":{"isSoftStop":true}}}`)...)
	assertFrameEvents(t, events, []FrameEvent{
		{Kind: "text", Content: "hello "},
		{Kind: "text", Content: "world"},
		{Kind: "soft_stop"},
	})

	adapter = NewStreamAdapter(StreamAdapterOptions{})
	events = mustFeed(t, adapter, `{"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img-card\",\"image_chunk\":{\"progress\":100,\"imageUuid\":\"uuid1\",\"imageUrl\":\"generated/foo.png\",\"moderated\":false}}"}}}}`)
	assertFrameEvents(t, events, []FrameEvent{
		{Kind: "image_progress", Content: "100", ImageID: "uuid1"},
		{Kind: "image", Content: "https://assets.grok.com/generated/foo.png", ImageID: "uuid1"},
	})

	adapter = NewStreamAdapter(StreamAdapterOptions{})
	mustFeed(t, adapter, `{"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"cite1\",\"url\":\"https://example.com/a\"}"},"webSearchResults":{"results":[{"url":"https://example.com/a","title":"Example A"}]}}}}`)
	events = mustFeed(t, adapter, `{"result":{"response":{"token":"See<grok:render card_id=\"cite1\" card_type=\"citation\" type=\"render_inline_citation\"></grok:render> now.","isThinking":false,"messageTag":"final"}}}`)
	assertFrameEvents(t, events, []FrameEvent{
		{Kind: "text", Content: "See [[1]](https://example.com/a) now."},
		{Kind: "annotation", AnnotationData: map[string]any{"type": "url_citation", "url": "https://example.com/a", "title": "Example A", "start_index": 3, "end_index": 32}},
	})
	assertAnyMaps(t, adapter.AnnotationsList(), []map[string]any{{"type": "url_citation", "url": "https://example.com/a", "title": "Example A", "start_index": 3, "end_index": 32}})
	assertAnyMaps(t, adapter.SearchSourcesList(), []map[string]any{{"url": "https://example.com/a", "title": "Example A", "type": "web"}})
}

func TestStreamAdapterMatchesPythonFalseyFinalMetadata(t *testing.T) {
	adapter := NewStreamAdapter(StreamAdapterOptions{})
	events := mustFeed(t, adapter, `{"result":{"response":{"finalMetadata":{}}}}`)
	assertFrameEvents(t, events, []FrameEvent{})

	events = mustFeed(t, adapter, `{"result":{"response":{"finalMetadata":{"done":true}}}}`)
	assertFrameEvents(t, events, []FrameEvent{{Kind: "soft_stop"}})
}

func TestStreamAdapterMatchesPythonCardSourceAndToolEdges(t *testing.T) {
	adapter := NewStreamAdapter(StreamAdapterOptions{})
	events := mustFeed(t, adapter, `{"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"img\",\"image_chunk\":{\"progress\":100,\"imageUuid\":\"u1\",\"imageUrl\":\"generated/a.png\",\"moderated\":true}}"}}}}`)
	assertFrameEvents(t, events, []FrameEvent{{Kind: "image_progress", Content: "100", ImageID: "u1"}})

	adapter = NewStreamAdapter(StreamAdapterOptions{})
	events = mustFeed(t, adapter, `{"result":{"response":{"cardAttachment":{"jsonData":"{\"id\":\"simg\",\"image\":{\"thumbnail\":\"https://img/t.png\",\"original\":\"https://img/o.png\",\"title\":\"T\",\"link\":\"https://page\"}}"},"token":"Look <grok:render card_id=\"simg\" card_type=\"image\" type=\"render_searched_image\"></grok:render>","isThinking":false,"messageTag":"final"}}}`)
	assertFrameEvents(t, events, []FrameEvent{{Kind: "text", Content: "Look [![T](https://img/t.png)](https://page)"}})

	adapter = NewStreamAdapter(StreamAdapterOptions{ShowSearchSources: true})
	mustFeed(t, adapter, `{"result":{"response":{"webSearchResults":{"results":[{"url":"https://a","title":"A [one]","type":"web"}]},"cardAttachment":{"jsonData":"{\"id\":\"c1\",\"url\":\"https://a\"}"}}}}`)
	events = mustFeed(t, adapter, `{"result":{"response":{"token":"See<grok:render card_id=\"c1\" card_type=\"citation\" type=\"render_inline_citation\"></grok:render> and again <grok:render card_id=\"c1\" card_type=\"citation\" type=\"render_inline_citation\"></grok:render>","isThinking":false,"messageTag":"final"}}}`)
	assertFrameEvents(t, events, []FrameEvent{
		{Kind: "text", Content: "See [[1]](https://a) and again "},
		{Kind: "annotation", AnnotationData: map[string]any{"type": "url_citation", "url": "https://a", "title": "A [one]", "start_index": 3, "end_index": 20}},
	})
	assertAnyMaps(t, adapter.SearchSourcesList(), []map[string]any{{"url": "https://a", "title": "A [one]", "type": "web"}})
	if got, want := adapter.ReferencesSuffix(), "\n\n## Sources\n[grok2api-sources]: #\n- [A \\[one\\]](https://a)\n"; got != want {
		t.Fatalf("sources suffix mismatch\nwant: %q\n got: %q", want, got)
	}

	adapter = NewStreamAdapter(StreamAdapterOptions{})
	mustFeed(t, adapter, `{"result":{"response":{"xSearchResults":{"results":[{"postId":"123","username":"bob","text":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}]}}}}`)
	assertAnyMaps(t, adapter.SearchSourcesList(), []map[string]any{{
		"url":   "https://x.com/bob/status/123",
		"title": "𝕏/@bob: abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWX...",
		"type":  "x_post",
	}})

	adapter = NewStreamAdapter(StreamAdapterOptions{})
	events = mustFeed(t, adapter, `{"result":{"response":{"messageTag":"tool_usage_card","rolloutId":"r1","toolUsageCard":{"toolUsageCardId":"t1","webSearch":{"args":{"query":"grok news","extra":"unused"}}}}}}`)
	assertFrameEvents(t, events, []FrameEvent{{Kind: "thinking", Content: "[r1] 🔍 web_search: grok news\n", RolloutID: "r1", MessageTag: "tool_usage_card"}})

	adapter = NewStreamAdapter(StreamAdapterOptions{})
	mustFeed(t, adapter, `{"result":{"response":{"token":"hi","isThinking":false,"messageTag":"final"}}}`)
	events = mustFeed(t, adapter, `{"result":{"response":{"messageTag":"tool_usage_card","rolloutId":"r1","toolUsageCard":{"toolUsageCardId":"t1","webSearch":{"args":{"query":"grok news"}}}}}}`)
	assertFrameEvents(t, events, []FrameEvent{})
}

func TestStreamAdapterMatchesPythonThinkingModes(t *testing.T) {
	stepID := 2
	adapter := NewStreamAdapter(StreamAdapterOptions{})
	events := mustFeed(t, adapter, `{"result":{"response":{"token":"- checking","isThinking":true,"messageTag":"thinking","rolloutId":"r1","messageStepId":2}}}`)
	assertFrameEvents(t, events, []FrameEvent{
		{Kind: "thinking", Content: "\n[r1]\n", RolloutID: "r1"},
		{Kind: "thinking", Content: "checking\n", RolloutID: "r1", MessageTag: "thinking", MessageStepID: &stepID},
	})

	adapter = NewStreamAdapter(StreamAdapterOptions{ThinkingSummary: true})
	events = mustFeed(t, adapter, `{"result":{"response":{"messageTag":"tool_usage_card","rolloutId":"Agent alpha","messageStepId":2,"toolUsageCard":{"toolUsageCardId":"t1","webSearch":{"args":{"query":"latest grok release"}}}}}}`)
	assertFrameEvents(t, events, []FrameEvent{})
	events = mustFeed(t, adapter, `{"result":{"response":{"finalMetadata":{"done":true}}}}`)
	assertFrameEvents(t, events, []FrameEvent{
		{Kind: "thinking", Content: "Research Scope\n", MessageTag: "summary"},
		{Kind: "thinking", Content: "- 已启动并行代理进行交叉检索与核验。\n", MessageTag: "summary"},
		{Kind: "thinking", Content: "- 并行检索：最新动态。\n", MessageTag: "summary"},
		{Kind: "soft_stop"},
	})
}

func mustFeed(t *testing.T, adapter *StreamAdapter, data string) []FrameEvent {
	t.Helper()
	events, err := adapter.Feed(data)
	if err != nil {
		t.Fatalf("Feed returned error: %v", err)
	}
	return events
}

func assertFrameEvents(t *testing.T, got, want []FrameEvent) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("events mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func assertAnyMaps(t *testing.T, got, want []map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("maps mismatch\nwant: %#v\n got: %#v", want, got)
	}
}
