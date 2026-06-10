package protocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const imageBaseURL = "https://assets.grok.com/"

var camelBoundaryRe = regexp.MustCompile(`([a-z0-9])([A-Z])`)

type StreamAdapterOptions struct {
	ThinkingSummary   bool
	ShowSearchSources bool
}

type imageURLRef struct {
	URL     string
	ImageID string
}

type pendingCitation struct {
	URL    string
	Title  string
	Needle string
}

type StreamAdapter struct {
	cardCache            map[string]map[string]any
	citationOrder        []string
	citationMap          map[string]int
	lastCitationIndex    int
	pendingCitations     []pendingCitation
	annotations          []map[string]any
	textOffset           int
	emittedReasoningKeys map[string]struct{}
	reasoning            *ReasoningAggregator
	summaryMode          bool
	showSearchSources    bool
	lastRollout          string
	contentStarted       bool
	webSearchResults     []map[string]any
	webSearchURLsSeen    map[string]struct{}
	ThinkingBuf          []string
	TextBuf              []string
	ImageURLs            []imageURLRef
}

func NewStreamAdapter(options StreamAdapterOptions) *StreamAdapter {
	var reasoning *ReasoningAggregator
	if options.ThinkingSummary {
		reasoning = NewReasoningAggregator()
	}
	return &StreamAdapter{
		cardCache:            map[string]map[string]any{},
		citationMap:          map[string]int{},
		lastCitationIndex:    -1,
		emittedReasoningKeys: map[string]struct{}{},
		reasoning:            reasoning,
		summaryMode:          options.ThinkingSummary,
		showSearchSources:    options.ShowSearchSources,
		webSearchURLsSeen:    map[string]struct{}{},
	}
}

func (a *StreamAdapter) ReferencesSuffix() string {
	if len(a.webSearchResults) == 0 || !a.showSearchSources {
		return ""
	}
	lines := []string{"\n\n## Sources", "[grok2api-sources]: #"}
	for _, item := range a.webSearchResults {
		title := stringFromAny(item["title"])
		if title == "" {
			title = stringFromAny(item["url"])
		}
		title = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(title, `\`, `\\`), "[", `\[`), "]", `\]`)
		lines = append(lines, fmt.Sprintf("- [%s](%s)", title, stringFromAny(item["url"])))
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *StreamAdapter) AnnotationsList() []map[string]any {
	return cloneAnyMaps(a.annotations)
}

func (a *StreamAdapter) SearchSourcesList() []map[string]any {
	if len(a.webSearchResults) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(a.webSearchResults))
	for _, item := range a.webSearchResults {
		title := stringFromAny(item["title"])
		if title == "" {
			title = stringFromAny(item["url"])
		}
		sourceType := stringFromAny(item["type"])
		if sourceType == "" {
			sourceType = "web"
		}
		out = append(out, map[string]any{"url": item["url"], "title": title, "type": sourceType})
	}
	return out
}

func (a *StreamAdapter) Feed(data string) ([]FrameEvent, error) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return nil, nil
	}
	if err := raiseForStreamErrorObject(obj); err != nil {
		return nil, err
	}
	result, ok := obj["result"].(map[string]any)
	if !ok || len(result) == 0 {
		return nil, nil
	}
	resp, ok := result["response"].(map[string]any)
	if !ok || len(resp) == 0 {
		return nil, nil
	}
	events := []FrameEvent{}
	if cardRaw, ok := resp["cardAttachment"].(map[string]any); ok && len(cardRaw) > 0 {
		events = append(events, a.handleCard(cardRaw)...)
	}
	a.collectSearchResults(resp)
	token, hasToken := resp["token"]
	think, _ := resp["isThinking"].(bool)
	tag := stringFromAny(resp["messageTag"])
	rollout := stringFromAny(resp["rolloutId"])
	stepID := intPointerFromAny(resp["messageStepId"])
	if tag == "tool_usage_card" {
		if a.contentStarted {
			return events, nil
		}
		if a.summaryMode {
			for _, line := range a.summarizeToolUsageSummary(resp, rollout, stepID) {
				a.appendReasoning(&events, line, rollout, tag, stepID)
			}
		} else if line := a.formatToolCard(resp, rollout); line != "" {
			if rollout != "" {
				a.lastRollout = rollout
			}
			a.appendReasoning(&events, line, rollout, tag, stepID)
		}
		return events, nil
	}
	if tag == "raw_function_result" {
		return events, nil
	}
	if _, ok := resp["toolUsageCardId"]; ok && resp["webSearchResults"] == nil && resp["codeExecutionResult"] == nil {
		return events, nil
	}
	if hasToken && think {
		return a.feedThinking(events, fmt.Sprint(token), tag, rollout, stepID), nil
	}
	if hasToken && !think && tag == "final" {
		a.contentStarted = true
		cleaned, localAnnotations := a.cleanToken(fmt.Sprint(token))
		if cleaned != "" {
			a.TextBuf = append(a.TextBuf, cleaned)
			events = append(events, FrameEvent{Kind: "text", Content: cleaned})
			for _, ann := range localAnnotations {
				ann["start_index"] = a.textOffset + ann["local_start"].(int)
				ann["end_index"] = a.textOffset + ann["local_end"].(int)
				delete(ann, "local_start")
				delete(ann, "local_end")
				a.annotations = append(a.annotations, cloneAnyMap(ann))
				events = append(events, FrameEvent{Kind: "annotation", AnnotationData: ann})
			}
			a.textOffset += len(cleaned)
		}
		return events, nil
	}
	if boolFromAny(resp["isSoftStop"]) || truthyAny(resp["finalMetadata"]) {
		a.flushPendingReasoning(&events)
		events = append(events, FrameEvent{Kind: "soft_stop"})
		return events, nil
	}
	return events, nil
}

func (a *StreamAdapter) feedThinking(events []FrameEvent, raw, tag, rollout string, stepID *int) []FrameEvent {
	if a.contentStarted {
		text := strings.TrimSpace(raw)
		if text != "" {
			if !strings.HasSuffix(raw, "\n") {
				raw += "\n"
			}
			a.ThinkingBuf = append(a.ThinkingBuf, raw)
		}
		return events
	}
	if a.summaryMode && a.reasoning != nil {
		for _, line := range a.reasoning.OnThinking(raw, ReasoningInputOptions{Tag: tag, Rollout: rollout, StepID: stepID}) {
			a.appendReasoning(&events, line, rollout, tag, stepID)
		}
		return events
	}
	if strings.HasPrefix(raw, "- ") {
		raw = strings.TrimSpace(raw[2:])
	}
	if raw == "" {
		return events
	}
	if rollout != "" && rollout != a.lastRollout {
		a.lastRollout = rollout
		header := "\n[" + rollout + "]\n"
		a.ThinkingBuf = append(a.ThinkingBuf, header)
		events = append(events, FrameEvent{Kind: "thinking", Content: header, RolloutID: rollout})
	}
	a.appendReasoning(&events, raw, rollout, tag, stepID)
	return events
}
