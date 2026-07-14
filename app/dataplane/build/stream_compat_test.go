package build

import (
	"strings"
	"testing"
)

func TestChatStreamFramesFromResponsesSSEDeltas(t *testing.T) {
	body := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"hel"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"lo"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed"}`,
		``,
	}, "\n")
	frames, err := ChatStreamFramesFromResponsesSSE("build/grok-4", "id1", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 3 {
		t.Fatalf("frames=%d %#v", len(frames), frames)
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, `"content":"hel"`) && !strings.Contains(joined, `"content":"hel"`) {
		// JSON marshal may not escape; check substrings
		if !strings.Contains(joined, "hel") || !strings.Contains(joined, "lo") {
			t.Fatalf("missing deltas: %s", joined)
		}
	}
	if !strings.Contains(joined, "chat.completion.chunk") {
		t.Fatalf("not openai chunk: %s", joined)
	}
	if frames[len(frames)-1] != "data: [DONE]\n\n" {
		t.Fatalf("last=%q", frames[len(frames)-1])
	}
}

func TestChatStreamFramesFromPlainJSON(t *testing.T) {
	frames, err := ChatStreamFramesFromResponsesSSE("build/x", "id2", strings.NewReader(`{"output_text":"pong"}`))
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(frames, "")
	if !strings.Contains(joined, "pong") {
		t.Fatalf("%s", joined)
	}
	if frames[len(frames)-1] != "data: [DONE]\n\n" {
		t.Fatalf("last=%q", frames[len(frames)-1])
	}
}
