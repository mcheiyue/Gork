package account

import (
	"reflect"
	"testing"
)

func TestDefaultQuotaSetValuesAndFreshCopy(t *testing.T) {
	basic := DefaultQuotaSet("basic")
	if basic.Auto.Total != 0 || basic.Fast.Total != 30 || basic.Fast.WindowSeconds != 86400 {
		t.Fatalf("basic defaults = %#v", basic)
	}
	if basic.Expert.Total != 0 || basic.Console == nil || basic.Console.Total != 30 || basic.Console.WindowSeconds != 900 {
		t.Fatalf("basic optional defaults = %#v", basic)
	}
	if basic.Heavy != nil || basic.Grok43 != nil {
		t.Fatalf("basic should not include heavy/grok_4_3: %#v", basic)
	}
	basic.Fast.Remaining = 1
	if again := DefaultQuotaSet("basic"); again.Fast.Remaining != 30 {
		t.Fatalf("DefaultQuotaSet returned shared state, fast remaining = %d", again.Fast.Remaining)
	}

	super := DefaultQuotaSet("super")
	if super.Auto.Total != 50 || super.Fast.Total != 140 || super.Expert.Total != 50 {
		t.Fatalf("super defaults = %#v", super)
	}
	if super.Grok43 == nil || super.Grok43.Total != 50 || super.Heavy != nil || super.Console != nil {
		t.Fatalf("super optional defaults = %#v", super)
	}
	super.Grok43.Remaining = 1
	if again := DefaultQuotaSet("super"); again.Grok43 == nil || again.Grok43.Remaining != 50 {
		t.Fatalf("DefaultQuotaSet returned shared optional state, grok_4_3 remaining = %#v", again.Grok43)
	}

	heavy := DefaultQuotaSet("heavy")
	if heavy.Auto.Total != 150 || heavy.Fast.Total != 400 || heavy.Expert.Total != 150 {
		t.Fatalf("heavy defaults = %#v", heavy)
	}
	if heavy.Heavy == nil || heavy.Heavy.Total != 20 || heavy.Grok43 == nil || heavy.Grok43.Total != 150 {
		t.Fatalf("heavy optional defaults = %#v", heavy)
	}
	if unknown := DefaultQuotaSet("unknown"); unknown.Fast.Total != 30 || unknown.Console == nil {
		t.Fatalf("unknown pool should fall back to basic defaults: %#v", unknown)
	}
}

func TestSupportedModeIDsAndDefaultWindow(t *testing.T) {
	tests := []struct {
		pool string
		want []int
	}{
		{pool: "basic", want: []int{1, 5}},
		{pool: "super", want: []int{0, 1, 2, 4, 5}},
		{pool: "heavy", want: []int{0, 1, 2, 3, 4, 5}},
		{pool: "unknown", want: []int{1, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.pool, func(t *testing.T) {
			if got := SupportedModeIDs(tt.pool); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SupportedModeIDs(%q) = %#v, want %#v", tt.pool, got, tt.want)
			}
		})
	}
	if !SupportsMode("super", 5) {
		t.Fatal("super should report mode 5 support")
	}
	if SupportsMode("unknown", 0) || !SupportsMode("unknown", 5) {
		t.Fatal("unknown pool should fall back to basic supported modes")
	}
	if got := DefaultQuotaWindow("super", 5); got != nil {
		t.Fatalf("DefaultQuotaWindow(super, 5) = %#v, want nil because no default console window", got)
	}
	if got := DefaultQuotaWindow("heavy", 3); got == nil || got.Total != 20 {
		t.Fatalf("DefaultQuotaWindow(heavy, 3) = %#v, want heavy quota", got)
	}
	if got := DefaultQuotaWindow("basic", 3); got != nil {
		t.Fatalf("DefaultQuotaWindow(basic, 3) = %#v, want nil", got)
	}
	if got := DefaultQuotaWindow("basic", 1); got == nil || got.Total != 30 || got.WindowSeconds != 86400 {
		t.Fatalf("DefaultQuotaWindow(basic, 1) = %#v", got)
	}
}

func TestNormalizeQuotaWindowAndSet(t *testing.T) {
	resetAt := int64(111)
	syncedAt := int64(222)
	window := QuotaWindow{
		Remaining:     99,
		Total:         99,
		WindowSeconds: 1,
		ResetAt:       &resetAt,
		SyncedAt:      &syncedAt,
		Source:        QuotaSourceReal,
	}
	fast := NormalizeQuotaWindow("basic", 1, &window)
	if fast == nil || fast.Remaining != 30 || fast.Total != 30 || fast.WindowSeconds != 86400 {
		t.Fatalf("NormalizeQuotaWindow basic fast = %#v", fast)
	}
	if fast.ResetAt == nil || *fast.ResetAt != resetAt || fast.SyncedAt == nil || *fast.SyncedAt != syncedAt || fast.Source != QuotaSourceReal {
		t.Fatalf("NormalizeQuotaWindow basic fast metadata = %#v", fast)
	}
	negative := window
	negative.Remaining = -10
	fast = NormalizeQuotaWindow("basic", 1, &negative)
	if fast == nil || fast.Remaining != 0 {
		t.Fatalf("NormalizeQuotaWindow basic fast should clamp negative remaining to 0, got %#v", fast)
	}
	if got := NormalizeQuotaWindow("basic", 0, &window); got != nil {
		t.Fatalf("NormalizeQuotaWindow basic auto = %#v, want nil", got)
	}
	if got := NormalizeQuotaWindow("unknown", 1, nil); got != nil {
		t.Fatalf("NormalizeQuotaWindow nil input = %#v, want nil", got)
	}
	if got := NormalizeQuotaWindow("super", 1, &window); got != &window {
		t.Fatalf("NormalizeQuotaWindow super fast should return original pointer")
	}

	input := AccountQuotaSet{Auto: window, Fast: window, Expert: window, Heavy: &window, Grok43: &window, Console: &window}
	normalized := NormalizeQuotaSet("basic", input)
	if normalized.Auto.Total != 0 || normalized.Expert.Total != 0 {
		t.Fatalf("NormalizeQuotaSet basic unsupported defaults = %#v", normalized)
	}
	if normalized.Fast.Total != 30 || normalized.Fast.WindowSeconds != 86400 {
		t.Fatalf("NormalizeQuotaSet basic fast = %#v", normalized.Fast)
	}
	if normalized.Heavy != nil || normalized.Grok43 != nil || normalized.Console == nil || normalized.Console.Total != 99 {
		t.Fatalf("NormalizeQuotaSet basic optional modes = %#v", normalized)
	}
	input.Console = nil
	normalized = NormalizeQuotaSet("basic", input)
	if normalized.Console == nil || normalized.Console.Total != 30 || normalized.Console.WindowSeconds != 900 {
		t.Fatalf("NormalizeQuotaSet basic missing console should fall back to default, got %#v", normalized.Console)
	}
	normalized = NormalizeQuotaSet("super", input)
	if normalized.Console != nil || normalized.Heavy != nil || normalized.Grok43 == nil || normalized.Grok43.Total != 99 {
		t.Fatalf("NormalizeQuotaSet super optional modes = %#v", normalized)
	}
}

func TestInferPoolFromAutoTotal(t *testing.T) {
	tests := []struct {
		total int
		want  string
	}{
		{total: 20, want: "basic"},
		{total: 50, want: "super"},
		{total: 150, want: "heavy"},
		{total: 999, want: "basic"},
	}
	for _, tt := range tests {
		got := InferPool(map[int]QuotaWindow{
			0: {Remaining: 1, Total: tt.total, WindowSeconds: 1, Source: QuotaSourceDefault},
		})
		if got != tt.want {
			t.Fatalf("InferPool(total=%d) = %q, want %q", tt.total, got, tt.want)
		}
	}
	if got := InferPool(map[int]QuotaWindow{}); got != "basic" {
		t.Fatalf("InferPool(missing auto) = %q, want basic", got)
	}
}
