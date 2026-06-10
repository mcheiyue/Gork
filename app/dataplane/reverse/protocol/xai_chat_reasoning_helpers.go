package protocol

import (
	"regexp"
	"sort"
	"strings"
)

var (
	reportSplitRe        = regexp.MustCompile(`(?:\n+|[。！？!?；;]+|\s+-\s+)`)
	reportPrefixRe       = regexp.MustCompile(`^(?:我知道|我收集了可靠信息|我收集到的?信息|从搜索结果总结|详细解释要点(?:（[^）]+）)?|补充)\s*`)
	reportEnglishPrefix  = regexp.MustCompile(`(?i)^(?:that|it shows|it seems)\s+`)
	reportNumberRe       = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	compactQueryRe       = regexp.MustCompile(`(?i)\b(?:or|and|site:[^\s]+|since:\S+|from:\S+|date:\S+)\b`)
	compactQueryQuotesRe = regexp.MustCompile(`[()"']`)
	whitespaceRe         = regexp.MustCompile(`\s+`)
	urlKeyRe             = regexp.MustCompile(`https?://\S+`)
	normalizeKeyRe       = regexp.MustCompile(`[^0-9A-Za-z_\x{4e00}-\x{9fff}]+`)
)

type reasoningReportEvent struct {
	section string
	text    string
	track   string
	level   int
}

func (a *ReasoningAggregator) emit(event ReasoningEvent) []string {
	text := strings.TrimSpace(event.Text)
	if text == "" {
		return nil
	}
	if event.Section == "scope" && (a.sectionSeen("evidence") || a.sectionSeen("finding")) {
		return nil
	}
	if event.Section == "evidence" && a.sectionSeen("finding") {
		if event.EvidenceLevel >= 4 || stringIn(event.Track, "latest_updates", "release_status", "official_confirmation", "specs_architecture", "v4_lite") {
			promotedKey := event.DedupeKey
			if promotedKey == "" {
				promotedKey = "evidence:" + a.normalizeKey(text)
			}
			event = ReasoningEvent{Section: "finding", Text: text, Track: event.Track, EvidenceLevel: event.EvidenceLevel, DedupeKey: "finding:promoted:" + promotedKey}
		} else {
			return nil
		}
	}
	dedupeKey := event.DedupeKey
	if dedupeKey == "" {
		dedupeKey = event.Section + ":" + a.normalizeKey(text)
	}
	if _, ok := a.emittedKeys[dedupeKey]; ok {
		return nil
	}
	if event.Track != "" {
		countKey := event.Section + "\x00" + event.Track
		emittedCount := a.trackEmitCounts[countKey]
		maxPerTrack := 2
		if event.Section == "scope" || event.Section == "evidence" {
			maxPerTrack = 1
		}
		if emittedCount >= maxPerTrack && !strings.HasSuffix(dedupeKey, "agents_started") {
			return nil
		}
		bestLevel := a.trackBestLevel[countKey]
		if _, ok := a.trackBestLevel[countKey]; ok && bestLevel > event.EvidenceLevel {
			return nil
		}
		if event.EvidenceLevel > bestLevel {
			a.trackBestLevel[countKey] = event.EvidenceLevel
		} else if _, ok := a.trackBestLevel[countKey]; !ok {
			a.trackBestLevel[countKey] = event.EvidenceLevel
		}
		a.trackEmitCounts[countKey] = emittedCount + 1
	}
	a.emittedKeys[dedupeKey] = struct{}{}
	lines := []string{}
	if !a.sectionSeen(event.Section) {
		a.sectionStarted[event.Section] = struct{}{}
		lines = append(lines, a.sectionTitle(event.Section)+"\n")
	}
	return append(lines, text+"\n")
}

func (a *ReasoningAggregator) observeLanguage(text string) {
	if text == "" {
		return
	}
	cjkCount, enCount := 0, 0
	for _, r := range text {
		if r >= '\u4e00' && r <= '\u9fff' {
			cjkCount++
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			enCount++
		}
	}
	if cjkCount >= 4 || cjkCount > maxInt(2, enCount/2) {
		a.zhVotes++
		if a.language == "" {
			a.language = "zh"
		}
		return
	}
	if enCount >= 4 {
		a.enVotes++
	}
}

func (a *ReasoningAggregator) extractReportEvents(message string) []ReasoningEvent {
	parts := reportSplitRe.Split(strings.ReplaceAll(message, `\n`, "\n"), -1)
	candidates := []struct {
		score  int
		clause string
	}{}
	for _, rawPart := range parts {
		clause := a.cleanReportClause(rawPart)
		if clause == "" {
			continue
		}
		if a.language == "zh" && !containsCJK(clause) {
			continue
		}
		if a.language == "en" && containsCJK(clause) {
			continue
		}
		score := a.scoreReportClause(clause)
		if score <= 0 {
			continue
		}
		candidates = append(candidates, struct {
			score  int
			clause string
		}{score: score, clause: clause})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return runeLen(candidates[i].clause) < runeLen(candidates[j].clause)
	})
	reports := a.selectReportEvents(candidates)
	out := make([]ReasoningEvent, 0, len(reports))
	for _, report := range reports {
		out = append(out, ReasoningEvent{
			Section:       report.section,
			Text:          report.text,
			Track:         report.track,
			EvidenceLevel: report.level,
			DedupeKey:     report.section + ":report:" + report.track + ":" + a.normalizeKey(report.text),
		})
	}
	return out
}

func (a *ReasoningAggregator) selectReportEvents(candidates []struct {
	score  int
	clause string
}) []reasoningReportEvent {
	results := []reasoningReportEvent{}
	seenLocal := map[string]struct{}{}
	seenTrackCounts := map[string]int{}
	for _, candidate := range candidates {
		key := a.normalizeKey(candidate.clause)
		if _, ok := seenLocal[key]; ok {
			continue
		}
		seenLocal[key] = struct{}{}
		track := a.inferTrack(candidate.clause)
		section := "evidence"
		if a.looksLikeFinding(candidate.clause) {
			section = "finding"
		}
		if a.isUnconfirmedSignal(candidate.clause) {
			section = "evidence"
		}
		trackKey := section + "\x00" + track
		if track == "" {
			trackKey = section + "\x00_"
		}
		maxTrackCount := 1
		if section == "finding" {
			maxTrackCount = 2
		}
		if seenTrackCounts[trackKey] >= maxTrackCount {
			continue
		}
		seenTrackCounts[trackKey]++
		defaultLevel := 2
		if section == "finding" {
			defaultLevel = 3
		}
		results = append(results, reasoningReportEvent{section: section, text: a.toBulletText(candidate.clause), track: track, level: a.inferEvidenceLevel(candidate.clause, defaultLevel)})
		if len(results) >= 6 {
			break
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		leftSection, rightSection := 1, 1
		if results[i].section == "evidence" {
			leftSection = 0
		}
		if results[j].section == "evidence" {
			rightSection = 0
		}
		if leftSection != rightSection {
			return leftSection < rightSection
		}
		if results[i].track != results[j].track {
			return results[i].track < results[j].track
		}
		return results[i].level > results[j].level
	})
	return results
}

func containsCJK(text string) bool {
	for _, r := range text {
		if r >= '\u4e00' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

func firstStringArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := args[key]; ok && value != nil {
			return strings.TrimSpace(toString(value))
		}
	}
	return ""
}

func toString(value any) string {
	return mapValueString(value)
}

func runeLen(text string) int {
	return len([]rune(text))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
