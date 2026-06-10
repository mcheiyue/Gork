package runtime

import (
	"testing"
	"time"
)

func TestClockConversionsMatchPythonSemantics(t *testing.T) {
	cases := []struct {
		name string
		ms   int64
		want int64
	}{
		{name: "zero", ms: 0, want: 0},
		{name: "subsecond positive", ms: 999, want: 0},
		{name: "exact second", ms: 1000, want: 1},
		{name: "negative floor division", ms: -1, want: -1},
		{name: "negative exact second", ms: -1000, want: -1},
		{name: "negative over second", ms: -1001, want: -2},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := MSToS(tt.ms); got != tt.want {
				t.Fatalf("MSToS(%d) = %d, want %d", tt.ms, got, tt.want)
			}
		})
	}

	if got := SToMS(-2); got != -2000 {
		t.Fatalf("SToMS(-2) = %d, want -2000", got)
	}
	if got := SToMS(3); got != 3000 {
		t.Fatalf("SToMS(3) = %d, want 3000", got)
	}
}

func TestNowValuesUseWallClockUnixTime(t *testing.T) {
	before := time.Now()
	gotS := NowS()
	gotMS := NowMS()
	after := time.Now()

	if gotS < before.Unix() || gotS > after.Unix() {
		t.Fatalf("NowS() = %d, want between %d and %d", gotS, before.Unix(), after.Unix())
	}
	if gotMS < before.UnixMilli() || gotMS > after.UnixMilli() {
		t.Fatalf("NowMS() = %d, want between %d and %d", gotMS, before.UnixMilli(), after.UnixMilli())
	}
}
