package model

import (
	"reflect"
	"testing"
)

func TestRegistryModelsMatchPythonSnapshot(t *testing.T) {
	want := expectedRegistryModels()
	if len(Models) != len(want) {
		t.Fatalf("Models length = %d, want %d", len(Models), len(want))
	}
	for i, expected := range want {
		if got := Models[i]; got != expected {
			t.Fatalf("Models[%d] = %#v, want %#v", i, got, expected)
		}
	}
}

func TestRegistryLookupMatchesPython(t *testing.T) {
	spec, ok := Get("grok-4.20-auto")
	if !ok {
		t.Fatalf("Get should find grok-4.20-auto")
	}
	if spec.ModeID != ModeAuto || spec.Tier != TierSuper || !spec.PreferBest {
		t.Fatalf("Get returned wrong spec: %#v", spec)
	}

	if _, ok := Get("missing-model"); ok {
		t.Fatalf("Get should report false for unknown models")
	}

	resolved, err := Resolve("grok-imagine-video")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolved.Capability != CapabilityVideo {
		t.Fatalf("Resolve returned %#v, want video capability", resolved)
	}

	if _, err := Resolve("missing-model"); err == nil || err.Error() != "Unknown model: 'missing-model'" {
		t.Fatalf("Resolve unknown error = %v", err)
	}
}

func TestRegistryListEnabledMatchesPython(t *testing.T) {
	got := ListEnabled()
	want := expectedRegistryModels()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListEnabled = %#v, want %#v", got, want)
	}

	got[0].ModelName = "mutated"
	if Models[0].ModelName == "mutated" {
		t.Fatalf("ListEnabled should return a copy, not the registry backing slice")
	}
}

func TestRegistryListByCapabilityMatchesPython(t *testing.T) {
	chatNames := modelNames(ListByCapability(CapabilityChat))
	wantChatNames := []string{
		"grok-4.20-0309-non-reasoning",
		"grok-4.20-0309",
		"grok-4.20-0309-reasoning",
		"grok-4.20-0309-non-reasoning-super",
		"grok-4.20-0309-super",
		"grok-4.20-0309-reasoning-super",
		"grok-4.20-0309-non-reasoning-heavy",
		"grok-4.20-0309-heavy",
		"grok-4.20-0309-reasoning-heavy",
		"grok-4.20-multi-agent-0309",
		"grok-4.20-fast",
		"grok-4.20-auto",
		"grok-4.20-expert",
		"grok-4.20-heavy",
		"grok-4.3-beta",
	}
	if !reflect.DeepEqual(chatNames, wantChatNames) {
		t.Fatalf("chat model names = %#v, want %#v", chatNames, wantChatNames)
	}

	chatOrImageNames := modelNames(ListByCapability(CapabilityChat | CapabilityImage))
	wantChatOrImageNames := append([]string{}, wantChatNames...)
	wantChatOrImageNames = append(wantChatOrImageNames,
		"grok-imagine-image-lite",
		"grok-imagine-image",
		"grok-imagine-image-pro",
	)
	if !reflect.DeepEqual(chatOrImageNames, wantChatOrImageNames) {
		t.Fatalf("chat or image model names = %#v, want %#v", chatOrImageNames, wantChatOrImageNames)
	}

	consoleNames := modelNames(ListByCapability(CapabilityConsoleChat))
	wantConsoleNames := []string{
		"grok-4.3-console",
		"grok-4.3-low",
		"grok-4.3-medium",
		"grok-4.3-high",
		"grok-4.20-0309-reasoning-console",
		"grok-4.20-0309-console",
		"grok-4.20-multi-agent-console",
		"grok-4.20-multi-agent-low",
		"grok-4.20-multi-agent-medium",
		"grok-4.20-multi-agent-high",
		"grok-4.20-multi-agent-xhigh",
		"grok-4.20-0309-non-reasoning-console",
		"grok-build-console",
	}
	if !reflect.DeepEqual(consoleNames, wantConsoleNames) {
		t.Fatalf("console model names = %#v, want %#v", consoleNames, wantConsoleNames)
	}

	if got := ListByCapability(CapabilityVoice); len(got) != 0 {
		t.Fatalf("voice model count = %d, want 0", len(got))
	}
}

func modelNames(specs []ModelSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.ModelName)
	}
	return names
}

func expectedRegistryModels() []ModelSpec {
	return []ModelSpec{
		{ModelName: "grok-4.20-0309-non-reasoning", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning", PreferBest: false},
		{ModelName: "grok-4.20-0309", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309", PreferBest: false},
		{ModelName: "grok-4.20-0309-reasoning", ModeID: ModeExpert, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning", PreferBest: false},
		{ModelName: "grok-4.20-0309-non-reasoning-super", ModeID: ModeFast, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning Super", PreferBest: false},
		{ModelName: "grok-4.20-0309-super", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Super", PreferBest: false},
		{ModelName: "grok-4.20-0309-reasoning-super", ModeID: ModeExpert, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning Super", PreferBest: false},
		{ModelName: "grok-4.20-0309-non-reasoning-heavy", ModeID: ModeFast, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning Heavy", PreferBest: false},
		{ModelName: "grok-4.20-0309-heavy", ModeID: ModeAuto, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Heavy", PreferBest: false},
		{ModelName: "grok-4.20-0309-reasoning-heavy", ModeID: ModeExpert, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning Heavy", PreferBest: false},
		{ModelName: "grok-4.20-multi-agent-0309", ModeID: ModeHeavy, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent 0309", PreferBest: false},
		{ModelName: "grok-4.20-fast", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Fast", PreferBest: true},
		{ModelName: "grok-4.20-auto", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Auto", PreferBest: true},
		{ModelName: "grok-4.20-expert", ModeID: ModeExpert, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Expert", PreferBest: true},
		{ModelName: "grok-4.20-heavy", ModeID: ModeHeavy, Tier: TierHeavy, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.20 Heavy", PreferBest: true},
		{ModelName: "grok-4.3-beta", ModeID: ModeGrok43, Tier: TierSuper, Capability: CapabilityChat, Enabled: true, PublicName: "Grok 4.3 Beta", PreferBest: false},
		{ModelName: "grok-imagine-image-lite", ModeID: ModeFast, Tier: TierBasic, Capability: CapabilityImage, Enabled: true, PublicName: "Grok Imagine Image Lite", PreferBest: false},
		{ModelName: "grok-imagine-image", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityImage, Enabled: true, PublicName: "Grok Imagine Image", PreferBest: false},
		{ModelName: "grok-imagine-image-pro", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityImage, Enabled: true, PublicName: "Grok Imagine Image Pro", PreferBest: false},
		{ModelName: "grok-imagine-image-edit", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityImageEdit, Enabled: true, PublicName: "Grok Imagine Image Edit", PreferBest: false},
		{ModelName: "grok-imagine-video", ModeID: ModeAuto, Tier: TierSuper, Capability: CapabilityVideo, Enabled: true, PublicName: "Grok Imagine Video", PreferBest: false},
		{ModelName: "grok-4.3-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 (Console)", PreferBest: false},
		{ModelName: "grok-4.3-low", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 Low Thinking", PreferBest: false},
		{ModelName: "grok-4.3-medium", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 Medium Thinking", PreferBest: false},
		{ModelName: "grok-4.3-high", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.3 High Thinking", PreferBest: false},
		{ModelName: "grok-4.20-0309-reasoning-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 0309 Reasoning (Console)", PreferBest: false},
		{ModelName: "grok-4.20-0309-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 0309 (Console)", PreferBest: false},
		{ModelName: "grok-4.20-multi-agent-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent (Console)", PreferBest: false},
		{ModelName: "grok-4.20-multi-agent-low", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent Low", PreferBest: false},
		{ModelName: "grok-4.20-multi-agent-medium", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent Medium", PreferBest: false},
		{ModelName: "grok-4.20-multi-agent-high", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent High", PreferBest: false},
		{ModelName: "grok-4.20-multi-agent-xhigh", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 Multi-Agent XHigh", PreferBest: false},
		{ModelName: "grok-4.20-0309-non-reasoning-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok 4.20 0309 Non-Reasoning (Console)", PreferBest: false},
		{ModelName: "grok-build-console", ModeID: ModeConsole, Tier: TierBasic, Capability: CapabilityConsoleChat, Enabled: true, PublicName: "Grok Build 0.1 (Console)", PreferBest: false},
	}
}
