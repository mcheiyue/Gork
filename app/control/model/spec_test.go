package model

import (
	"reflect"
	"testing"
)

func TestModelSpecFieldsAndCapabilityHelpersMatchPython(t *testing.T) {
	spec := ModelSpec{
		ModelName:  "grok-3",
		ModeID:     ModeExpert,
		Tier:       TierSuper,
		Capability: CapabilityChat | CapabilityImageEdit | CapabilityConsoleChat,
		Enabled:    true,
		PublicName: "Grok 3",
		PreferBest: true,
	}

	if spec.ModelName != "grok-3" || spec.ModeID != ModeExpert || spec.Tier != TierSuper ||
		spec.Capability != CapabilityChat|CapabilityImageEdit|CapabilityConsoleChat ||
		!spec.Enabled || spec.PublicName != "Grok 3" || !spec.PreferBest {
		t.Fatalf("ModelSpec fields were not retained: %#v", spec)
	}

	if !spec.IsChat() {
		t.Fatalf("IsChat should detect CapabilityChat")
	}
	if spec.IsImage() {
		t.Fatalf("IsImage should be false without CapabilityImage")
	}
	if !spec.IsImageEdit() {
		t.Fatalf("IsImageEdit should detect CapabilityImageEdit")
	}
	if spec.IsVideo() {
		t.Fatalf("IsVideo should be false without CapabilityVideo")
	}
	if spec.IsVoice() {
		t.Fatalf("IsVoice should be false without CapabilityVoice")
	}
	if !spec.IsConsoleChat() {
		t.Fatalf("IsConsoleChat should detect CapabilityConsoleChat")
	}
}

func TestModelSpecPoolNameAndIDMatchPython(t *testing.T) {
	cases := []struct {
		tier Tier
		name string
		id   int
	}{
		{TierBasic, "basic", 0},
		{TierSuper, "super", 1},
		{TierHeavy, "heavy", 2},
	}

	for _, tt := range cases {
		spec := ModelSpec{Tier: tt.tier}
		if got := spec.PoolName(); got != tt.name {
			t.Fatalf("tier %d PoolName = %q, want %q", tt.tier, got, tt.name)
		}
		if got := spec.PoolID(); got != tt.id {
			t.Fatalf("tier %d PoolID = %d, want %d", tt.tier, got, tt.id)
		}
	}
}

func TestModelSpecPoolCandidatesMatchPythonPriorityOrder(t *testing.T) {
	cases := []struct {
		name       string
		tier       Tier
		preferBest bool
		want       []int
	}{
		{"basic default", TierBasic, false, []int{0, 1, 2}},
		{"super default", TierSuper, false, []int{1, 2}},
		{"heavy default", TierHeavy, false, []int{2}},
		{"basic prefer best", TierBasic, true, []int{2, 1, 0}},
		{"super prefer best", TierSuper, true, []int{2, 1}},
		{"heavy prefer best", TierHeavy, true, []int{2}},
	}

	for _, tt := range cases {
		spec := ModelSpec{Tier: tt.tier, PreferBest: tt.preferBest}
		if got := spec.PoolCandidates(); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("%s PoolCandidates = %#v, want %#v", tt.name, got, tt.want)
		}
	}
}
