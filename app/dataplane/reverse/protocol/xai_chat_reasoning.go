package protocol

import (
	"fmt"
	"strings"
)

type ReasoningEvent struct {
	Section       string
	Text          string
	Track         string
	EvidenceLevel int
	DedupeKey     string
}

type ReasoningInputOptions struct {
	Tag     string
	Rollout string
	StepID  *int
}

type ReasoningAggregator struct {
	language           string
	enVotes            int
	zhVotes            int
	agentSearchStarted bool
	emittedKeys        map[string]struct{}
	pendingEvents      []ReasoningEvent
	sectionStarted     map[string]struct{}
	trackBestLevel     map[string]int
	trackEmitCounts    map[string]int
}

func NewReasoningAggregator() *ReasoningAggregator {
	return &ReasoningAggregator{
		emittedKeys:     map[string]struct{}{},
		pendingEvents:   []ReasoningEvent{},
		sectionStarted:  map[string]struct{}{},
		trackBestLevel:  map[string]int{},
		trackEmitCounts: map[string]int{},
	}
}

func (a *ReasoningAggregator) OnThinking(token string, options ReasoningInputOptions) []string {
	a.observeLanguage(token)
	tagName := strings.TrimSpace(options.Tag)
	text := strings.TrimSpace(token)
	if text == "" {
		return nil
	}
	var event ReasoningEvent
	var ok bool
	if tagName == "header" {
		event, ok = a.normalizeHeader(text, stepValue(options.StepID))
	} else {
		event, ok = a.normalizeSummary(text, stepValue(options.StepID))
	}
	if !ok {
		return nil
	}
	return a.dispatch(event)
}

func (a *ReasoningAggregator) OnToolUsage(toolName string, args map[string]any, options ReasoningInputOptions) []string {
	lines := []string{}
	a.observeLanguage(firstStringArg(args, "query", "message", "instructions"))
	switch toolName {
	case "web_search":
		query := strings.TrimSpace(firstStringArg(args, "query", "q"))
		if query == "" {
			return nil
		}
		if strings.HasPrefix(options.Rollout, "Agent") && !a.agentSearchStarted {
			a.agentSearchStarted = true
			lines = append(lines, a.dispatch(ReasoningEvent{
				Section:   "scope",
				Text:      a.localizedLine("agents_started"),
				DedupeKey: "scope:agents_started",
			})...)
		}
		track := a.inferTrack(query)
		if track == "" {
			return lines
		}
		return append(lines, a.dispatch(ReasoningEvent{
			Section:       "scope",
			Text:          a.localizedTrackLine(track),
			Track:         track,
			EvidenceLevel: 1,
			DedupeKey:     "scope:web:" + track,
		})...)
	case "x_search", "x_keyword_search", "x_semantic_search":
		track := a.inferTrack(strings.TrimSpace(firstStringArg(args, "query")))
		if track == "" {
			return nil
		}
		return a.dispatch(ReasoningEvent{
			Section:       "evidence",
			Text:          a.localizedSocialLine(track),
			Track:         track,
			EvidenceLevel: 2,
			DedupeKey:     "evidence:social:" + track,
		})
	case "browse_page":
		sourceKind, track := a.classifyPageSource(strings.TrimSpace(firstStringArg(args, "url")), args)
		if sourceKind == "" {
			return nil
		}
		level := 3
		if sourceKind == "official" || sourceKind == "product" {
			level = 4
		}
		eventTrack := track
		if eventTrack == "" {
			eventTrack = sourceKind
		}
		return a.dispatch(ReasoningEvent{
			Section:       "evidence",
			Text:          a.localizedBrowseLine(sourceKind, track),
			Track:         eventTrack,
			EvidenceLevel: level,
			DedupeKey:     fmt.Sprintf("evidence:browse:%s:%s", sourceKind, track),
		})
	case "search_images", "image_search":
		description := strings.TrimSpace(firstStringArg(args, "image_description", "imageDescription"))
		if description == "" {
			return nil
		}
		topic := a.classifyImageTopic(description)
		if topic == "" {
			return nil
		}
		return a.dispatch(ReasoningEvent{
			Section:       "scope",
			Text:          a.localizedImageLine(topic),
			Track:         "visual_assets",
			EvidenceLevel: 1,
			DedupeKey:     "scope:image:" + topic,
		})
	case "chatroom_send":
		message := strings.TrimSpace(firstStringArg(args, "message"))
		if message == "" {
			return nil
		}
		for _, event := range a.extractReportEvents(message) {
			lines = append(lines, a.dispatch(event)...)
		}
		return lines
	case "code_execution":
		return a.dispatch(ReasoningEvent{
			Section:   "evidence",
			Text:      a.localizedLine("code_execution"),
			DedupeKey: "evidence:code_execution",
		})
	default:
		return nil
	}
}

func (a *ReasoningAggregator) Finalize() []string {
	if len(a.pendingEvents) == 0 {
		return nil
	}
	if a.language == "" {
		if a.enVotes > 0 && a.zhVotes == 0 {
			a.language = "en"
		} else {
			a.language = "zh"
		}
	}
	return a.flushPending()
}

func (a *ReasoningAggregator) normalizeHeader(text string, stepID int) (ReasoningEvent, bool) {
	stripped := strings.TrimSpace(text)
	if _, ok := reasoningGenericHeaders[strings.ToLower(stripped)]; ok {
		return ReasoningEvent{}, false
	}
	section := "evidence"
	if !a.looksLikeVerification(stripped) && stepID <= 1 {
		section = "understanding"
	}
	return ReasoningEvent{Section: section, Text: a.toBulletText(stripped), DedupeKey: section + ":header:" + a.normalizeKey(stripped)}, true
}

func (a *ReasoningAggregator) normalizeSummary(text string, stepID int) (ReasoningEvent, bool) {
	summary := strings.TrimSpace(strings.TrimLeft(text, "- "))
	if summary == "" || strings.HasPrefix(summary, "建议搜索") || strings.HasPrefix(summary, "正在调用工具搜索") {
		return ReasoningEvent{}, false
	}
	track := a.inferTrack(summary)
	if a.looksLikeProgress(summary) {
		section := "scope"
		level := 1
		if a.looksLikeVerification(summary) {
			section = "evidence"
			level = 2
		}
		return ReasoningEvent{Section: section, Text: a.toBulletText(summary), Track: track, EvidenceLevel: level, DedupeKey: section + ":summary:" + a.normalizeKey(summary)}, true
	}
	if a.looksLikeFinding(summary) {
		if a.isUnconfirmedSignal(summary) {
			return ReasoningEvent{Section: "evidence", Text: a.toBulletText(summary), Track: track, EvidenceLevel: 2, DedupeKey: "evidence:summary:" + a.normalizeKey(summary)}, true
		}
		if !a.agentSearchStarted && stepID <= 1 {
			return ReasoningEvent{Section: "understanding", Text: a.toBulletText(summary), Track: track, EvidenceLevel: 2, DedupeKey: "understanding:summary:" + a.normalizeKey(summary)}, true
		}
		return ReasoningEvent{Section: "finding", Text: a.toBulletText(summary), Track: track, EvidenceLevel: 3, DedupeKey: "finding:summary:" + a.normalizeKey(summary)}, true
	}
	section := "scope"
	if stepID <= 1 {
		section = "understanding"
	}
	return ReasoningEvent{Section: section, Text: a.toBulletText(summary), Track: track, EvidenceLevel: 1, DedupeKey: section + ":summary:" + a.normalizeKey(summary)}, true
}

func (a *ReasoningAggregator) dispatch(event ReasoningEvent) []string {
	if a.language == "" {
		a.pendingEvents = append(a.pendingEvents, event)
		if a.zhVotes > 0 {
			a.language = "zh"
		} else if a.enVotes >= 3 {
			a.language = "en"
		} else if len(a.pendingEvents) < 4 {
			return nil
		} else {
			a.language = "en"
		}
		return a.flushPending()
	}
	lines := []string{}
	if len(a.pendingEvents) > 0 {
		lines = append(lines, a.flushPending()...)
	}
	return append(lines, a.emit(event)...)
}

func (a *ReasoningAggregator) flushPending() []string {
	pending := a.pendingEvents
	a.pendingEvents = nil
	lines := []string{}
	for _, event := range pending {
		lines = append(lines, a.emit(event)...)
	}
	return lines
}

func stepValue(stepID *int) int {
	if stepID == nil {
		return 0
	}
	return *stepID
}
