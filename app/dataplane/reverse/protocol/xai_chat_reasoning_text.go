package protocol

import (
	"fmt"
	"strings"
)

func (a *ReasoningAggregator) cleanReportClause(rawPart string) string {
	clause := strings.Trim(whitespaceRe.ReplaceAllString(rawPart, " "), " -•\t")
	if clause == "" {
		return ""
	}
	delimiter := ""
	if strings.Contains(clause, "：") {
		delimiter = "："
	} else if strings.Contains(clause, ":") {
		delimiter = ":"
	}
	if delimiter != "" {
		parts := strings.SplitN(clause, delimiter, 2)
		head := strings.TrimSpace(parts[0])
		headLower := strings.ToLower(head)
		if runeLen(head) <= 18 || stringInSubstring(headLower, "总结", "最新", "关键", "补充", "latest", "summary", "note") {
			clause = strings.TrimSpace(parts[1])
		}
	}
	clause = strings.Trim(clause, " -•\t")
	clause = reportPrefixRe.ReplaceAllString(clause, "")
	clause = reportEnglishPrefix.ReplaceAllString(clause, "")
	if runeLen(clause) < 8 {
		return ""
	}
	lowered := strings.ToLower(clause)
	for _, prefix := range reasoningLowValuePrefixes {
		if strings.HasPrefix(lowered, prefix) {
			return ""
		}
	}
	if strings.Contains(clause, "?") || strings.Contains(clause, "？") {
		return ""
	}
	return a.compactText(clause, 120)
}

func (a *ReasoningAggregator) scoreReportClause(clause string) int {
	lowered := strings.ToLower(clause)
	score := 0
	if stringInSubstring(lowered, reasoningFindingHints...) {
		score += 3
	}
	if reportNumberRe.MatchString(clause) {
		score += 2
	}
	if stringInSubstring(clause, "月", "日", "年", "小时", "分钟") {
		score++
	}
	if stringInSubstring(clause, "重要", "航运", "渔业", "发电", "生态", "模式", "视觉") {
		score++
	}
	if stringInSubstring(lowered, "可能", "rumor", "传闻", "widely believed", "believed") {
		score--
	}
	if stringInSubstring(lowered, "可以", "suggest", "建议", "should", "friendly", "reply") {
		score -= 2
	}
	if runeLen(clause) > 150 {
		score--
	}
	return score
}

func (a *ReasoningAggregator) inferEvidenceLevel(clause string, defaultLevel int) int {
	lowered := strings.ToLower(clause)
	if stringInSubstring(lowered, "官网", "official", "chat ui", "界面更新", "页面") {
		return 4
	}
	if stringInSubstring(lowered, "x平台", "x posts", "社区", "widely believed", "传闻", "rumor") {
		return maxInt(2, defaultLevel-1)
	}
	return defaultLevel
}

func (a *ReasoningAggregator) isUnconfirmedSignal(clause string) bool {
	return stringInSubstring(strings.ToLower(clause),
		"x平台", "x posts", "社区", "community", "widely believed", "believed",
		"传闻", "rumor", "曝光", "泄露",
	)
}

func (a *ReasoningAggregator) toBulletText(text string) string {
	stripped := strings.TrimSpace(text)
	if strings.HasPrefix(stripped, "- ") {
		stripped = strings.TrimSpace(stripped[2:])
	}
	return "- " + a.ensureTerminalPunctuation(stripped)
}

func (a *ReasoningAggregator) ensureTerminalPunctuation(text string) string {
	stripped := strings.TrimSpace(text)
	if stripped == "" {
		return ""
	}
	if strings.HasSuffix(stripped, "。") || strings.HasSuffix(stripped, "！") || strings.HasSuffix(stripped, "？") ||
		strings.HasSuffix(stripped, ".") || strings.HasSuffix(stripped, "!") || strings.HasSuffix(stripped, "?") {
		return stripped
	}
	if containsCJK(stripped) {
		return stripped + "。"
	}
	return stripped + "."
}

func (a *ReasoningAggregator) compactQuery(text string) string {
	cleaned := compactQueryRe.ReplaceAllString(text, " ")
	cleaned = compactQueryQuotesRe.ReplaceAllString(cleaned, " ")
	return strings.TrimSpace(whitespaceRe.ReplaceAllString(cleaned, " "))
}

func (a *ReasoningAggregator) compactText(text string, limit int) string {
	compact := strings.TrimSpace(whitespaceRe.ReplaceAllString(text, " "))
	if runeLen(compact) <= limit {
		return compact
	}
	runes := []rune(compact)
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func (a *ReasoningAggregator) normalizeKey(text string) string {
	lowered := strings.ToLower(text)
	lowered = urlKeyRe.ReplaceAllString(lowered, "")
	return normalizeKeyRe.ReplaceAllString(lowered, "")
}

func (a *ReasoningAggregator) sectionSeen(section string) bool {
	_, ok := a.sectionStarted[section]
	return ok
}

func stringIn(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func stringInSubstring(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func mapValueString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}
