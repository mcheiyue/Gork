package account

import (
	"fmt"
	"strconv"
	"strings"
)

type QuotaWindow struct {
	Remaining     int         `json:"remaining"`
	Total         int         `json:"total"`
	WindowSeconds int         `json:"window_seconds"`
	ResetAt       *int64      `json:"reset_at"`
	SyncedAt      *int64      `json:"synced_at"`
	Source        QuotaSource `json:"source"`
}

func (w QuotaWindow) IsExhausted() bool {
	return w.Remaining <= 0
}

func (w QuotaWindow) IsWindowExpired(now int64) bool {
	return w.ResetAt != nil && now >= *w.ResetAt
}

func (w QuotaWindow) ToDict() map[string]any {
	return map[string]any{
		"remaining":      w.Remaining,
		"total":          w.Total,
		"window_seconds": w.WindowSeconds,
		"reset_at":       optionalInt64Value(w.ResetAt),
		"synced_at":      optionalInt64Value(w.SyncedAt),
		"source":         int(w.Source),
	}
}

func QuotaWindowFromDict(data map[string]any) (QuotaWindow, error) {
	remaining, err := intFromAny(data["remaining"], 0)
	if err != nil {
		return QuotaWindow{}, fmt.Errorf("remaining: %w", err)
	}
	total, err := intFromAny(data["total"], 0)
	if err != nil {
		return QuotaWindow{}, fmt.Errorf("total: %w", err)
	}
	return quotaWindowFromParsed(data, remaining, total)
}

func quotaWindowFromParsed(data map[string]any, remaining int, total int) (QuotaWindow, error) {
	windowSeconds, err := intFromAny(data["window_seconds"], 0)
	if err != nil {
		return QuotaWindow{}, fmt.Errorf("window_seconds: %w", err)
	}
	resetAt, err := optionalInt64FromAny(data["reset_at"])
	if err != nil {
		return QuotaWindow{}, fmt.Errorf("reset_at: %w", err)
	}
	syncedAt, err := optionalInt64FromAny(data["synced_at"])
	if err != nil {
		return QuotaWindow{}, fmt.Errorf("synced_at: %w", err)
	}
	source, err := quotaSourceFromAny(data["source"])
	if err != nil {
		return QuotaWindow{}, err
	}
	return QuotaWindow{remaining, total, windowSeconds, resetAt, syncedAt, source}, nil
}

type AccountQuotaSet struct {
	Auto    QuotaWindow  `json:"auto"`
	Fast    QuotaWindow  `json:"fast"`
	Expert  QuotaWindow  `json:"expert"`
	Heavy   *QuotaWindow `json:"heavy,omitempty"`
	Grok43  *QuotaWindow `json:"grok_4_3,omitempty"`
	Console *QuotaWindow `json:"console,omitempty"`
}

func (s *AccountQuotaSet) Get(modeID int) *QuotaWindow {
	switch modeID {
	case 0:
		return &s.Auto
	case 1:
		return &s.Fast
	case 2:
		return &s.Expert
	case 3:
		return s.Heavy
	case 4:
		return s.Grok43
	case 5:
		return s.Console
	default:
		return nil
	}
}

func (s *AccountQuotaSet) Set(modeID int, window QuotaWindow) {
	switch modeID {
	case 0:
		s.Auto = window
	case 1:
		s.Fast = window
	case 2:
		s.Expert = window
	case 3:
		s.Heavy = &window
	case 4:
		s.Grok43 = &window
	default:
		s.Console = &window
	}
}

func (s AccountQuotaSet) ToDict() map[string]any {
	data := map[string]any{
		"auto":   s.Auto.ToDict(),
		"fast":   s.Fast.ToDict(),
		"expert": s.Expert.ToDict(),
	}
	addQuotaWindow(data, "heavy", s.Heavy)
	addQuotaWindow(data, "grok_4_3", s.Grok43)
	addQuotaWindow(data, "console", s.Console)
	return data
}

func AccountQuotaSetFromDict(data map[string]any) (AccountQuotaSet, error) {
	auto, err := requiredQuotaWindowAt(data, "auto")
	if err != nil {
		return AccountQuotaSet{}, err
	}
	fast, err := requiredQuotaWindowAt(data, "fast")
	if err != nil {
		return AccountQuotaSet{}, err
	}
	expert, err := requiredQuotaWindowAt(data, "expert")
	if err != nil {
		return AccountQuotaSet{}, err
	}
	return optionalQuotaSetFromDict(data, auto, fast, expert)
}

func optionalQuotaSetFromDict(data map[string]any, auto, fast, expert QuotaWindow) (AccountQuotaSet, error) {
	heavy, err := quotaWindowAt(data, "heavy")
	if err != nil {
		return AccountQuotaSet{}, err
	}
	grok43, err := quotaWindowAt(data, "grok_4_3")
	if err != nil {
		return AccountQuotaSet{}, err
	}
	console, err := quotaWindowAt(data, "console")
	if err != nil {
		return AccountQuotaSet{}, err
	}
	return AccountQuotaSet{Auto: auto, Fast: fast, Expert: expert, Heavy: heavy, Grok43: grok43, Console: console}, nil
}

func addQuotaWindow(data map[string]any, key string, window *QuotaWindow) {
	if window != nil {
		data[key] = window.ToDict()
	}
}

func requiredQuotaWindowAt(data map[string]any, key string) (QuotaWindow, error) {
	window, err := quotaWindowAt(data, key)
	if err != nil {
		return QuotaWindow{}, err
	}
	if window == nil {
		return QuotaWindowFromDict(map[string]any{})
	}
	return *window, nil
}

func quotaWindowAt(data map[string]any, key string) (*QuotaWindow, error) {
	nested, ok, err := mapAt(data, key)
	if err != nil || !ok {
		return nil, err
	}
	window, err := QuotaWindowFromDict(nested)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return &window, nil
}

func mapAt(data map[string]any, key string) (map[string]any, bool, error) {
	value, ok := data[key]
	if !ok || value == nil {
		return map[string]any{}, false, nil
	}
	nested, err := mapFromAny(value)
	if err != nil || len(nested) == 0 {
		return nested, false, err
	}
	return nested, true, nil
}

func intFromAny(value any, defaultValue int) (int, error) {
	if value == nil {
		return defaultValue, nil
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(v))
	default:
		return 0, fmt.Errorf("unsupported int value %T", value)
	}
}

func optionalInt64FromAny(value any) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := int64FromAny(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func int64FromAny(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported int64 value %T", value)
	}
}

func optionalInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func quotaSourceFromAny(value any) (QuotaSource, error) {
	parsed, err := intFromAny(value, int(QuotaSourceDefault))
	if err != nil {
		return QuotaSourceDefault, fmt.Errorf("source: %w", err)
	}
	source := QuotaSource(parsed)
	if source != QuotaSourceDefault && source != QuotaSourceReal && source != QuotaSourceEstimated {
		return QuotaSourceDefault, fmt.Errorf("source: unknown quota source %d", parsed)
	}
	return source, nil
}

func mapFromAny(value any) (map[string]any, error) {
	switch v := value.(type) {
	case map[string]any:
		return v, nil
	case map[string]map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			out[key] = nested
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported map value %T", value)
	}
}
