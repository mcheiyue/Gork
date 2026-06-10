package model

import (
	"reflect"
	"testing"
)

func TestModeIDValuesAndAPIStringsMatchPython(t *testing.T) {
	cases := []struct {
		mode ModeID
		into int
		api  string
	}{
		{ModeAuto, 0, "auto"},
		{ModeFast, 1, "fast"},
		{ModeExpert, 2, "expert"},
		{ModeHeavy, 3, "heavy"},
		{ModeGrok43, 4, "grok-420-computer-use-sa"},
		{ModeConsole, 5, "console"},
	}
	for _, tt := range cases {
		if got := int(tt.mode); got != tt.into {
			t.Fatalf("mode int = %d, want %d", got, tt.into)
		}
		if got := tt.mode.ToAPIString(); got != tt.api {
			t.Fatalf("mode %d ToAPIString = %q, want %q", tt.mode, got, tt.api)
		}
	}
}

func TestTierAndCapabilityValuesMatchPython(t *testing.T) {
	if TierBasic != 0 || TierSuper != 1 || TierHeavy != 2 {
		t.Fatalf("tier values changed: basic=%d super=%d heavy=%d", TierBasic, TierSuper, TierHeavy)
	}
	if CapabilityChat != 1 || CapabilityImage != 2 || CapabilityImageEdit != 4 ||
		CapabilityVideo != 8 || CapabilityVoice != 16 || CapabilityAsset != 32 || CapabilityConsoleChat != 64 {
		t.Fatalf("capability bit values changed")
	}
	if CapabilityChat|CapabilityImageEdit != 5 {
		t.Fatalf("capability values should behave as bit flags")
	}
}

func TestModeCollectionsMatchPythonOrder(t *testing.T) {
	if !reflect.DeepEqual(AllModes, []ModeID{ModeAuto, ModeFast, ModeExpert}) {
		t.Fatalf("AllModes = %#v", AllModes)
	}
	if !reflect.DeepEqual(AllModesWithHeavy, []ModeID{ModeAuto, ModeFast, ModeExpert, ModeHeavy}) {
		t.Fatalf("AllModesWithHeavy = %#v", AllModesWithHeavy)
	}
	if !reflect.DeepEqual(AllModesFull, []ModeID{ModeAuto, ModeFast, ModeExpert, ModeHeavy, ModeGrok43}) {
		t.Fatalf("AllModesFull = %#v", AllModesFull)
	}
	if got := ModeStrings[ModeHeavy]; got != "heavy" {
		t.Fatalf("ModeStrings[ModeHeavy] = %q, want heavy", got)
	}
}
