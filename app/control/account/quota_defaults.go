package account

const (
	basicFastLimit            = 30
	basicFastWindowSeconds    = 86400
	basicConsoleLimit         = 30
	basicConsoleWindowSeconds = 900
)

var (
	basicQuotaDefaults = AccountQuotaSet{
		Auto:    quotaWindow(0, 0, 0),
		Fast:    quotaWindow(basicFastLimit, basicFastLimit, basicFastWindowSeconds),
		Expert:  quotaWindow(0, 0, 0),
		Console: quotaWindowPtr(basicConsoleLimit, basicConsoleLimit, basicConsoleWindowSeconds),
	}
	superQuotaDefaults = AccountQuotaSet{
		Auto:   quotaWindow(50, 50, 7200),
		Fast:   quotaWindow(140, 140, 7200),
		Expert: quotaWindow(50, 50, 7200),
		Grok43: quotaWindowPtr(50, 50, 7200),
	}
	heavyQuotaDefaults = AccountQuotaSet{
		Auto:   quotaWindow(150, 150, 7200),
		Fast:   quotaWindow(400, 400, 7200),
		Expert: quotaWindow(150, 150, 7200),
		Heavy:  quotaWindowPtr(20, 20, 7200),
		Grok43: quotaWindowPtr(150, 150, 7200),
	}
)

var supportedModeIDsByPool = map[string]map[int]bool{
	"basic": {1: true, 5: true},
	"super": {0: true, 1: true, 2: true, 4: true, 5: true},
	"heavy": {0: true, 1: true, 2: true, 3: true, 4: true, 5: true},
}

var autoTotalToPool = map[int]string{
	20:  "basic",
	50:  "super",
	150: "heavy",
}

func DefaultQuotaSet(pool string) AccountQuotaSet {
	return cloneQuotaSet(quotaDefaultsForPool(pool))
}

func SupportsMode(pool string, modeID int) bool {
	return supportedModesForPool(pool)[modeID]
}

func SupportedModeIDs(pool string) []int {
	supported := supportedModesForPool(pool)
	out := []int{}
	for _, modeID := range []int{0, 1, 2, 3, 4, 5} {
		if supported[modeID] {
			out = append(out, modeID)
		}
	}
	return out
}

func DefaultQuotaWindow(pool string, modeID int) *QuotaWindow {
	if !SupportsMode(pool, modeID) {
		return nil
	}
	defaults := DefaultQuotaSet(pool)
	return defaults.Get(modeID)
}

func NormalizeQuotaWindow(pool string, modeID int, window *QuotaWindow) *QuotaWindow {
	if window == nil || !SupportsMode(pool, modeID) {
		return nil
	}
	if pool == "basic" && modeID == 1 {
		return normalizeBasicFastWindow(window)
	}
	return window
}

func NormalizeQuotaSet(pool string, quotaSet AccountQuotaSet) AccountQuotaSet {
	defaults := DefaultQuotaSet(pool)
	auto := windowOrDefault(NormalizeQuotaWindow(pool, 0, &quotaSet.Auto), defaults.Auto)
	fast := windowOrDefault(NormalizeQuotaWindow(pool, 1, &quotaSet.Fast), defaults.Fast)
	expert := windowOrDefault(NormalizeQuotaWindow(pool, 2, &quotaSet.Expert), defaults.Expert)
	return AccountQuotaSet{
		Auto:    auto,
		Fast:    fast,
		Expert:  expert,
		Heavy:   NormalizeQuotaWindow(pool, 3, quotaSet.Heavy),
		Grok43:  NormalizeQuotaWindow(pool, 4, quotaSet.Grok43),
		Console: windowPtrOrDefault(NormalizeQuotaWindow(pool, 5, quotaSet.Console), defaults.Console),
	}
}

func InferPool(windows map[int]QuotaWindow) string {
	auto, ok := windows[0]
	if !ok {
		return "basic"
	}
	if pool, ok := autoTotalToPool[auto.Total]; ok {
		return pool
	}
	return "basic"
}

func quotaDefaultsForPool(pool string) AccountQuotaSet {
	switch pool {
	case "super":
		return superQuotaDefaults
	case "heavy":
		return heavyQuotaDefaults
	default:
		return basicQuotaDefaults
	}
}

func supportedModesForPool(pool string) map[int]bool {
	if supported, ok := supportedModeIDsByPool[pool]; ok {
		return supported
	}
	return supportedModeIDsByPool["basic"]
}

func normalizeBasicFastWindow(window *QuotaWindow) *QuotaWindow {
	normalized := QuotaWindow{
		Remaining:     clampInt(window.Remaining, 0, basicFastLimit),
		Total:         basicFastLimit,
		WindowSeconds: basicFastWindowSeconds,
		ResetAt:       cloneInt64Ptr(window.ResetAt),
		SyncedAt:      cloneInt64Ptr(window.SyncedAt),
		Source:        window.Source,
	}
	return &normalized
}

func cloneQuotaSet(src AccountQuotaSet) AccountQuotaSet {
	return AccountQuotaSet{
		Auto:    cloneQuotaWindow(src.Auto),
		Fast:    cloneQuotaWindow(src.Fast),
		Expert:  cloneQuotaWindow(src.Expert),
		Heavy:   cloneQuotaWindowPtr(src.Heavy),
		Grok43:  cloneQuotaWindowPtr(src.Grok43),
		Console: cloneQuotaWindowPtr(src.Console),
	}
}

func cloneQuotaWindow(window QuotaWindow) QuotaWindow {
	window.ResetAt = cloneInt64Ptr(window.ResetAt)
	window.SyncedAt = cloneInt64Ptr(window.SyncedAt)
	return window
}

func cloneQuotaWindowPtr(window *QuotaWindow) *QuotaWindow {
	if window == nil {
		return nil
	}
	cloned := cloneQuotaWindow(*window)
	return &cloned
}

func windowOrDefault(window *QuotaWindow, fallback QuotaWindow) QuotaWindow {
	if window == nil {
		return fallback
	}
	return *window
}

func windowPtrOrDefault(window *QuotaWindow, fallback *QuotaWindow) *QuotaWindow {
	if window == nil {
		return fallback
	}
	return window
}

func quotaWindow(remaining, total, windowSeconds int) QuotaWindow {
	return QuotaWindow{
		Remaining:     remaining,
		Total:         total,
		WindowSeconds: windowSeconds,
		Source:        QuotaSourceDefault,
	}
}

func quotaWindowPtr(remaining, total, windowSeconds int) *QuotaWindow {
	window := quotaWindow(remaining, total, windowSeconds)
	return &window
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
