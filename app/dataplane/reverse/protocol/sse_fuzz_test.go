package protocol

import (
	"strings"
	"testing"
)

func FuzzParseSSEEventLines(f *testing.F) {
	for _, seed := range []string{
		"data: hello\n\n",
		"event: done\ndata: [DONE]\n\n",
		": comment\ndata: {\"ok\":true}\n\n",
		"event: update\r\ndata: one\r\ndata: two\r\n\r\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		event, data, done := ParseSSEEventLines(strings.Split(raw, "\n"))
		if event == "" {
			t.Fatalf("event should not be empty")
		}
		if done && data != "" {
			t.Fatalf("done event returned data %q", data)
		}
	})
}
