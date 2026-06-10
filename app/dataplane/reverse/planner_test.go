package reverse

import (
	"testing"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	reverseruntime "github.com/jiujiu532/grok2api/app/dataplane/reverse/runtime"
)

func TestBuildPlanMatchesPythonPlannerByCapability(t *testing.T) {
	tests := []struct {
		name      string
		spec      controlmodel.ModelSpec
		endpoint  string
		transport TransportKind
		timeout   float64
		content   string
	}{
		{
			name: "chat",
			spec: controlmodel.ModelSpec{
				ModeID:     controlmodel.ModeExpert,
				Tier:       controlmodel.TierSuper,
				Capability: controlmodel.CapabilityChat,
			},
			endpoint:  reverseruntime.Chat,
			transport: TransportKindHTTPSSE,
			timeout:   120.0,
			content:   "application/json",
		},
		{
			name: "image",
			spec: controlmodel.ModelSpec{
				ModeID:     controlmodel.ModeFast,
				Tier:       controlmodel.TierBasic,
				Capability: controlmodel.CapabilityImage,
			},
			endpoint:  reverseruntime.WSImagine,
			transport: TransportKindWebSocket,
			timeout:   300.0,
			content:   "application/json",
		},
		{
			name: "image edit",
			spec: controlmodel.ModelSpec{
				ModeID:     controlmodel.ModeAuto,
				Tier:       controlmodel.TierBasic,
				Capability: controlmodel.CapabilityImageEdit,
			},
			endpoint:  reverseruntime.Chat,
			transport: TransportKindHTTPSSE,
			timeout:   120.0,
			content:   "application/json",
		},
		{
			name: "video",
			spec: controlmodel.ModelSpec{
				ModeID:     controlmodel.ModeHeavy,
				Tier:       controlmodel.TierHeavy,
				Capability: controlmodel.CapabilityVideo,
			},
			endpoint:  reverseruntime.MediaPost,
			transport: TransportKindHTTPJSON,
			timeout:   30.0,
			content:   "application/json",
		},
		{
			name: "voice",
			spec: controlmodel.ModelSpec{
				ModeID:     controlmodel.ModeAuto,
				Tier:       controlmodel.TierBasic,
				Capability: controlmodel.CapabilityVoice,
			},
			endpoint:  reverseruntime.Chat,
			transport: TransportKindHTTPSSE,
			timeout:   120.0,
			content:   "application/json",
		},
		{
			name: "fallback",
			spec: controlmodel.ModelSpec{
				ModeID: controlmodel.ModeAuto,
				Tier:   controlmodel.TierBasic,
			},
			endpoint:  reverseruntime.Chat,
			transport: TransportKindHTTPSSE,
			timeout:   120.0,
			content:   "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := BuildPlan(tt.spec)
			if plan.Endpoint != tt.endpoint || plan.TransportKind != tt.transport {
				t.Fatalf("endpoint/transport = %q/%v, want %q/%v", plan.Endpoint, plan.TransportKind, tt.endpoint, tt.transport)
			}
			if plan.TimeoutS != tt.timeout || plan.ContentType != tt.content {
				t.Fatalf("timeout/content = %v/%q, want %v/%q", plan.TimeoutS, plan.ContentType, tt.timeout, tt.content)
			}
			if plan.ModeID != int(tt.spec.ModeID) {
				t.Fatalf("mode id = %d, want %d", plan.ModeID, tt.spec.ModeID)
			}
			wantPool := tt.spec.PoolCandidates()
			if len(plan.PoolCandidates) != len(wantPool) {
				t.Fatalf("pool candidates = %#v, want %#v", plan.PoolCandidates, wantPool)
			}
			for i := range wantPool {
				if plan.PoolCandidates[i] != wantPool[i] {
					t.Fatalf("pool candidates = %#v, want %#v", plan.PoolCandidates, wantPool)
				}
			}
		})
	}
}

func TestBuildPlanMatchesPythonPlannerChatPriority(t *testing.T) {
	spec := controlmodel.ModelSpec{
		ModeID:     controlmodel.ModeExpert,
		Tier:       controlmodel.TierSuper,
		Capability: controlmodel.CapabilityChat | controlmodel.CapabilityImage | controlmodel.CapabilityVideo,
	}

	plan := BuildPlan(spec, BuildPlanOptions{Request: map[string]any{"prompt": "hi"}})

	if plan.Endpoint != reverseruntime.Chat || plan.TransportKind != TransportKindHTTPSSE {
		t.Fatalf("endpoint/transport = %q/%v, want chat/http_sse", plan.Endpoint, plan.TransportKind)
	}
	if plan.TimeoutS != 120.0 || plan.ContentType != "application/json" {
		t.Fatalf("timeout/content = %v/%q, want 120/application-json", plan.TimeoutS, plan.ContentType)
	}
}

func TestBuildPlanMatchesPythonReversePlanDefaults(t *testing.T) {
	spec := controlmodel.ModelSpec{
		ModeID:     controlmodel.ModeAuto,
		Tier:       controlmodel.TierBasic,
		Capability: controlmodel.CapabilityImage,
	}

	plan := BuildPlan(spec, BuildPlanOptions{Request: map[string]any{"ignored": true}})

	if plan.Origin != "https://grok.com" || plan.Referer != "https://grok.com/" {
		t.Fatalf("origin/referer = %q/%q", plan.Origin, plan.Referer)
	}
	if plan.Extra == nil || len(plan.Extra) != 0 {
		t.Fatalf("extra = %#v, want empty map", plan.Extra)
	}
}
