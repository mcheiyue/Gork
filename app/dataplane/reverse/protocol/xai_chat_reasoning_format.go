package protocol

import (
	"fmt"
	"strings"
)

func (a *ReasoningAggregator) sectionTitle(section string) string {
	labels := reasoningZHLabels
	if a.language == "en" {
		labels = reasoningENLabels
	}
	if label, ok := labels[section]; ok {
		return label
	}
	return section
}

func (a *ReasoningAggregator) localizedLine(key string) string {
	if a.language == "en" {
		return map[string]string{
			"agents_started": "- Parallel agents have started cross-checking the topic.",
			"code_execution": "- Executing code or generating runnable content.",
		}[key]
	}
	return map[string]string{
		"agents_started": "- 已启动并行代理进行交叉检索与核验。",
		"code_execution": "- 正在执行代码或生成可运行内容。",
	}[key]
}

func (a *ReasoningAggregator) localizedTrackLine(track string) string {
	label := a.trackLabel(track)
	if a.language == "en" {
		return fmt.Sprintf("- Parallel research: %s.", label)
	}
	return fmt.Sprintf("- 并行检索：%s。", label)
}

func (a *ReasoningAggregator) localizedSocialLine(track string) string {
	label := a.trackLabel(track)
	if a.language == "en" {
		return fmt.Sprintf("- Social cross-check: %s.", label)
	}
	return fmt.Sprintf("- 社媒交叉核验：%s。", label)
}

func (a *ReasoningAggregator) localizedBrowseLine(sourceKind, track string) string {
	trackLabel := ""
	if track != "" {
		trackLabel = a.trackLabel(track)
	}
	mapping := map[string]string{
		"official":  "页面核对：官网与官方页面",
		"product":   "页面核对：产品页面与实际界面",
		"community": "页面核对：公开报道与社区文章",
	}
	if a.language == "en" {
		mapping = map[string]string{
			"official":  "Page verification: official site and official pages",
			"product":   "Page verification: product page and live UI",
			"community": "Page verification: public reports and community write-ups",
		}
	}
	base := mapping[sourceKind]
	if trackLabel != "" {
		if a.language == "en" {
			return fmt.Sprintf("- %s, focusing on %s.", base, trackLabel)
		}
		return fmt.Sprintf("- %s，重点核对%s。", base, trackLabel)
	}
	if a.language == "en" {
		return "- " + base + "."
	}
	return "- " + base + "。"
}

func (a *ReasoningAggregator) localizedImageLine(topic string) string {
	if a.language == "en" {
		return map[string]string{
			"diagram": "- Visual asset search: diagrams and explanatory graphics.",
			"photo":   "- Visual asset search: real-world comparison photos.",
			"generic": "- Visual asset search: supporting image references.",
		}[topic]
	}
	return map[string]string{
		"diagram": "- 视觉素材检索：示意图与结构说明素材。",
		"photo":   "- 视觉素材检索：实景对比图片。",
		"generic": "- 视觉素材检索：补充说明图片。",
	}[topic]
}

func (a *ReasoningAggregator) trackLabel(track string) string {
	labels := reasoningZHLabels
	if a.language == "en" {
		labels = reasoningENLabels
	}
	if label, ok := labels[track]; ok {
		return label
	}
	return track
}

func (a *ReasoningAggregator) inferTrack(text string) string {
	lowered := strings.ToLower(a.compactQuery(text))
	if lowered == "" {
		return ""
	}
	for _, rule := range reasoningTrackRules {
		for _, keyword := range rule.keywords {
			if strings.Contains(lowered, keyword) {
				return rule.track
			}
		}
	}
	return ""
}

func (a *ReasoningAggregator) classifyPageSource(url string, args map[string]any) (string, string) {
	lowered := strings.ToLower(url)
	instructions := firstStringArg(args, "instructions")
	track := a.pickBrowseTrack(url + " " + instructions)
	if strings.Contains(lowered, "deepseek.ai") || strings.Contains(lowered, "deepseek.com") {
		if strings.Contains(lowered, "chat.deepseek.com") || strings.Contains(lowered, "platform.deepseek.com") {
			if track == "" {
				track = "ui_modes"
			}
			return "product", track
		}
		if track == "" {
			track = "official_confirmation"
		}
		return "official", track
	}
	if url != "" {
		return "community", track
	}
	return "", track
}

func (a *ReasoningAggregator) pickBrowseTrack(text string) string {
	lowered := strings.ToLower(a.compactQuery(text))
	priority := []struct {
		track    string
		keywords []string
	}{
		{"ui_modes", []string{"expert", "vision", "mode", "界面", "ui"}},
		{"release_status", []string{"release", "released", "launch", "发布", "上线", "status"}},
		{"specs_architecture", []string{"spec", "parameter", "architecture", "context", "engram", "moe", "规格", "参数", "架构", "上下文"}},
		{"v4_lite", []string{"v4 lite", "sealion", "sealion-lite", "海狮"}},
		{"official_confirmation", []string{"official", "官网", "current models", "offering"}},
	}
	for _, rule := range priority {
		for _, keyword := range rule.keywords {
			if strings.Contains(lowered, keyword) {
				return rule.track
			}
		}
	}
	return a.inferTrack(text)
}

func (a *ReasoningAggregator) classifyImageTopic(text string) string {
	lowered := strings.ToLower(text)
	if stringInSubstring(lowered, "diagram", "示意图", "bulge") {
		return "diagram"
	}
	if stringInSubstring(lowered, "photo", "照片", "real", "high tide", "low tide", "高潮", "低潮") {
		return "photo"
	}
	return "generic"
}

func (a *ReasoningAggregator) looksLikeProgress(text string) bool {
	return stringInSubstring(strings.ToLower(text), reasoningProgressiveHints...)
}

func (a *ReasoningAggregator) looksLikeVerification(text string) bool {
	return stringInSubstring(strings.ToLower(text), "确认", "核对", "浏览", "整合", "比对", "check", "verify", "browse", "integrat")
}

func (a *ReasoningAggregator) looksLikeFinding(text string) bool {
	if a.looksLikeProgress(text) {
		return false
	}
	return stringInSubstring(strings.ToLower(text), reasoningFindingHints...)
}
