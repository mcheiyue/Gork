package protocol

var reasoningGenericHeaders = map[string]struct{}{
	"":                            {},
	"thinking about your request": {},
}

var reasoningProgressiveHints = []string{
	"正在", "准备", "计划", "查找", "搜索", "浏览", "确认", "核对", "整合", "挖掘", "比对",
	"checking", "browsing", "verifying", "integrating", "digging", "cross-checking", "searching", "planning",
}

var reasoningFindingHints = []string{
	"尚未", "已经", "已", "确认", "表明", "说明", "显示", "主要", "通常", "支持", "出现", "启动",
	"持续", "提升", "更新", "灰度", "发布", "上线", "多模态", "视觉", "专家", "context", "token",
	"参数", "每天", "大潮", "小潮", "半日潮", "引力", "周期", "模式", "confirmed", "launched",
	"released", "rollout", "testing", "native multimodal", "widely believed", "latest",
}

var reasoningLowValuePrefixes = []string{
	"用户", "user", "i can", "我可以", "我收集", "建议", "need", "需要", "应该", "since instructions",
	"proposed", "mermaid", "可以用", "我建议",
}

var reasoningTrackRules = []struct {
	track    string
	keywords []string
}{
	{"latest_updates", []string{"最新", "latest", "today", "recent", "最近", "update", "news", "本周", "4月", "april"}},
	{"release_status", []string{"release date", "released", "release", "launch", "上线", "发布", "正式发布", "current status"}},
	{"gray_rollout", []string{"灰度", "grayscale", "gray release", "灰度测试", "内测", "rollout"}},
	{"official_confirmation", []string{"official", "官网", "official site", "site:", "platform.deepseek.com", "deepseek.ai"}},
	{"ui_modes", []string{"vision", "视觉", "expert", "专家模式", "fast", "default", "ui", "界面", "mode"}},
	{"v4_lite", []string{"v4 lite", "sealion", "sealion-lite", "海狮"}},
	{"specs_architecture", []string{"specs", "parameters", "architecture", "engram", "mhc", "moe", "context", "benchmarks", "规格", "参数", "架构", "万亿"}},
	{"definition_basics", []string{"定义", "解释", "什么是", "what is", "phenomenon", "现象"}},
	{"causes_mechanism", []string{"成因", "原因", "cause", "causes", "gravity", "引力", "机制"}},
	{"categories_types", []string{"春潮", "小潮", "半日潮", "全日潮", "类型", "分类"}},
	{"impacts_applications", []string{"影响", "应用", "发电", "航运", "生活", "生态"}},
}

var reasoningZHLabels = map[string]string{
	"understanding":         "理解问题",
	"scope":                 "检索范围",
	"evidence":              "核验与证据",
	"finding":               "关键发现",
	"latest_updates":        "最新动态",
	"release_status":        "发布状态与上线节奏",
	"gray_rollout":          "灰度进展",
	"official_confirmation": "官方渠道确认",
	"ui_modes":              "Expert / Vision 模式关联",
	"v4_lite":               "V4 Lite 与 Sealion 线索",
	"specs_architecture":    "规格、架构与上下文能力",
	"definition_basics":     "定义与基础解释",
	"causes_mechanism":      "成因与机制",
	"categories_types":      "分类与相关类型",
	"impacts_applications":  "影响与应用",
}

var reasoningENLabels = map[string]string{
	"understanding":         "Understanding",
	"scope":                 "Research Scope",
	"evidence":              "Verification",
	"finding":               "Key Findings",
	"latest_updates":        "latest updates",
	"release_status":        "release status and rollout timing",
	"gray_rollout":          "gray rollout progress",
	"official_confirmation": "official confirmation",
	"ui_modes":              "Expert / Vision mode signals",
	"v4_lite":               "V4 Lite and Sealion clues",
	"specs_architecture":    "specs, architecture, and context capability",
	"definition_basics":     "definition and basic explanation",
	"causes_mechanism":      "causes and mechanism",
	"categories_types":      "categories and related types",
	"impacts_applications":  "impacts and applications",
}
