package openai

import (
	"testing"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

func TestToolSieveStreamsSafeTextAndBuffersPartialOpenTag(t *testing.T) {
	sieve := NewToolSieve([]string{"search"})

	safe, calls := sieve.Feed("hello <too")
	if safe != "hello " || calls != nil {
		t.Fatalf("first feed safe=%q calls=%#v", safe, calls)
	}

	safe, calls = sieve.Feed("l_calls><tool_call><tool_name>search</tool_name><parameters>{\"query\":\"golang\",\"limit\":2}</parameters>")
	if safe != "" || calls != nil {
		t.Fatalf("capturing feed safe=%q calls=%#v", safe, calls)
	}

	safe, calls = sieve.Feed("</tool_call></tool_calls>")
	if safe != "" {
		t.Fatalf("closing feed safe=%q", safe)
	}
	assertOneSearchCall(t, calls)

	safe, calls = sieve.Feed(" tail")
	if safe != " tail" || calls != nil {
		t.Fatalf("after done safe=%q calls=%#v", safe, calls)
	}
}

func TestToolSieveFlushReportsBufferedToolSyntaxWithoutCalls(t *testing.T) {
	sieve := NewToolSieve([]string{"search"})

	safe, calls := sieve.Feed("prefix <tool_calls><tool_call><tool_name>search</tool_name><parameters>{\"query\":\"golang\",\"limit\":2}</parameters></tool_call>")
	if safe != "prefix " || calls != nil {
		t.Fatalf("feed safe=%q calls=%#v", safe, calls)
	}

	calls = sieve.Flush()
	if calls == nil || len(calls) != 0 {
		t.Fatalf("flush calls=%#v, want empty non-nil slice", calls)
	}
	if calls = sieve.Flush(); calls != nil {
		t.Fatalf("second flush calls=%#v", calls)
	}
}

func TestToolSieveFlushIgnoresPlainBufferedPrefix(t *testing.T) {
	sieve := NewToolSieve([]string{"search"})

	safe, calls := sieve.Feed("ordinary <to")
	if safe != "ordinary " || calls != nil {
		t.Fatalf("feed safe=%q calls=%#v", safe, calls)
	}
	if calls = sieve.Flush(); calls != nil {
		t.Fatalf("flush calls=%#v", calls)
	}
}

func TestToolSieveMatchesToolCallsTagsCaseInsensitively(t *testing.T) {
	sieve := NewToolSieve([]string{"search"})

	safe, calls := sieve.Feed("prefix <TOOL_CALLS><tool_call><tool_name>search</tool_name><parameters>{\"query\":\"golang\",\"limit\":2}</parameters></tool_call></TOOL_CALLS>")
	if safe != "prefix " {
		t.Fatalf("safe=%q", safe)
	}
	assertOneSearchCall(t, calls)
}

func assertOneSearchCall(t *testing.T, calls []protocol.ParsedToolCall) {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("calls=%#v, want one", calls)
	}
	call := calls[0]
	if call.CallID == "" || call.Name != "search" || call.Arguments != `{"limit":2,"query":"golang"}` {
		t.Fatalf("call=%#v", call)
	}
}
