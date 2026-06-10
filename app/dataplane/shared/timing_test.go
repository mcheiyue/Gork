package shared

import (
	"testing"
	"time"
)

func TestTimingConversionsMirrorPlatformClock(t *testing.T) {
	if got := MSToS(1500); got != 1 {
		t.Fatalf("MSToS(1500) = %d, want 1", got)
	}
	if got := MSToS(-1001); got != -2 {
		t.Fatalf("MSToS(-1001) = %d, want -2", got)
	}
	if got := SToMS(3); got != 3000 {
		t.Fatalf("SToMS(3) = %d, want 3000", got)
	}
}

func TestTimingNowValuesUseWallClock(t *testing.T) {
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
