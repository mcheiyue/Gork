package protocol

import (
	"reflect"
	"testing"
)

func TestReasoningAggregatorMatchesPythonThinkingFixtures(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnThinking("Thinking about your request", ReasoningInputOptions{Tag: "header", StepID: intRef(1)})...)
	got = append(got, agg.OnThinking("I am searching latest Grok updates from official sources.", ReasoningInputOptions{Tag: "summary", StepID: intRef(2)})...)
	got = append(got, agg.OnThinking("Verified: xAI released Grok 3 with improved reasoning.", ReasoningInputOptions{Tag: "summary", StepID: intRef(3)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"Research Scope\n",
		"- I am searching latest Grok updates from official sources.\n",
		"Key Findings\n",
		"- Verified: xAI released Grok 3 with improved reasoning.\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonToolFixtures(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnToolUsage("web_search", map[string]any{"query": "latest Grok model release notes"}, ReasoningInputOptions{Rollout: "Agent alpha", StepID: intRef(2)})...)
	got = append(got, agg.OnToolUsage("browse_page", map[string]any{"url": "https://x.ai/news/grok-3"}, ReasoningInputOptions{StepID: intRef(3)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"Research Scope\n",
		"- 已启动并行代理进行交叉检索与核验。\n",
		"- 并行检索：最新动态。\n",
		"Verification\n",
		"- 页面核对：公开报道与社区文章，重点核对最新动态。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonChineseFixtures(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnThinking("我正在搜索最新发布信息，并核对官方来源。", ReasoningInputOptions{Tag: "summary", StepID: intRef(2)})...)
	got = append(got, agg.OnToolUsage("web_search", map[string]any{"query": "Grok 最新 发布"}, ReasoningInputOptions{StepID: intRef(2)})...)
	got = append(got, agg.OnThinking("确认：官方页面显示该功能已经上线。", ReasoningInputOptions{Tag: "summary", StepID: intRef(3)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"核验与证据\n",
		"- 我正在搜索最新发布信息，并核对官方来源。\n",
		"- 确认：官方页面显示该功能已经上线。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorExtractsChatroomReportFindings(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnThinking("我正在整理官方页面和社区反馈。", ReasoningInputOptions{Tag: "summary", StepID: intRef(2)})...)
	got = append(got, agg.OnToolUsage("chatroom_send", map[string]any{
		"message": "最新总结：官方页面显示 2025 年 4 月已经发布；社区传闻仍需要核对；建议继续搜索更多资料；关键补充：视觉模式支持图片理解。",
	}, ReasoningInputOptions{StepID: intRef(3)})...)
	got = append(got, agg.OnToolUsage("code_execution", map[string]any{}, ReasoningInputOptions{StepID: intRef(4)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"检索范围\n",
		"- 我正在整理官方页面和社区反馈。\n",
		"关键发现\n",
		"- 官方页面显示 2025 年 4 月已经发布。\n",
		"- 视觉模式支持图片理解。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorDedupesRepeatedTrackEvents(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	for range 2 {
		got = append(got, agg.OnToolUsage("web_search", map[string]any{"query": "latest Grok release"}, ReasoningInputOptions{StepID: intRef(2)})...)
	}
	got = append(got, agg.Finalize()...)

	want := []string{
		"Research Scope\n",
		"- 并行检索：最新动态。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorFinalizesBufferedEnglishSummary(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnThinking("Checking sources for latest release notes", ReasoningInputOptions{Tag: "summary", StepID: intRef(1)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"Verification\n",
		"- Checking sources for latest release notes.\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonLateScopeAndStrongEvidenceRules(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnThinking("我正在整理官方页面。", ReasoningInputOptions{Tag: "summary", StepID: intRef(1)})...)
	got = append(got, agg.OnToolUsage("chatroom_send", map[string]any{
		"message": "关键发现：视觉模式对发电和生态影响重要。",
	}, ReasoningInputOptions{StepID: intRef(2)})...)
	got = append(got, agg.OnToolUsage("browse_page", map[string]any{"url": "https://chat.deepseek.com/"}, ReasoningInputOptions{StepID: intRef(3)})...)
	got = append(got, agg.OnToolUsage("web_search", map[string]any{"query": "Grok 最新 发布"}, ReasoningInputOptions{StepID: intRef(4)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"检索范围\n",
		"- 我正在整理官方页面。\n",
		"关键发现\n",
		"- 视觉模式对发电和生态影响重要。\n",
		"- 页面核对：产品页面与实际界面，重点核对Expert / Vision 模式关联。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonImageSearchTopicCaps(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnToolUsage("image_search", map[string]any{"image_description": "diagram showing model architecture"}, ReasoningInputOptions{StepID: intRef(1)})...)
	got = append(got, agg.OnToolUsage("search_images", map[string]any{"imageDescription": "real photo high tide visual evidence"}, ReasoningInputOptions{StepID: intRef(2)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"检索范围\n",
		"- 视觉素材检索：示意图与结构说明素材。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonSocialAndCommunityBrowse(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnToolUsage("x_search", map[string]any{"query": "community rumor about grok release"}, ReasoningInputOptions{StepID: intRef(1)})...)
	got = append(got, agg.OnToolUsage("browse_page", map[string]any{
		"url":          "https://example.com/community/grok",
		"instructions": "community rumor page",
	}, ReasoningInputOptions{StepID: intRef(2)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"Verification\n",
		"- 社媒交叉核验：发布状态与上线节奏。\n",
		"- 页面核对：公开报道与社区文章。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonSearchQueryCompaction(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnToolUsage("web_search", map[string]any{
		"query": "site:x.ai latest AND release from:xai",
	}, ReasoningInputOptions{StepID: intRef(1)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"Research Scope\n",
		"- 并行检索：最新动态。\n",
	}
	assertReasoningLines(t, got, want)
}

func TestReasoningAggregatorMatchesPythonReportCleaningAndScoring(t *testing.T) {
	agg := NewReasoningAggregator()
	got := []string{}
	got = append(got, agg.OnThinking("我正在整合报告。", ReasoningInputOptions{Tag: "summary", StepID: intRef(1)})...)
	got = append(got, agg.OnToolUsage("chatroom_send", map[string]any{
		"message": "总结：官方页面确认 3 月 12 日已上线。\n建议：可以回复用户。\n社区传闻称可能还在灰度。\n关键发现：视觉模式对发电和生态影响重要。\n这是一个问题吗？",
	}, ReasoningInputOptions{StepID: intRef(2)})...)
	got = append(got, agg.Finalize()...)

	want := []string{
		"核验与证据\n",
		"- 我正在整合报告。\n",
		"- 社区传闻称可能还在灰度。\n",
		"- 官方页面确认 3 月 12 日已上线。\n",
		"关键发现\n",
		"- 视觉模式对发电和生态影响重要。\n",
	}
	assertReasoningLines(t, got, want)
}

func assertReasoningLines(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("reasoning lines mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func intRef(v int) *int {
	return &v
}
