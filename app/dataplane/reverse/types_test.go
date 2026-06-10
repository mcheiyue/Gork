package reverse

import (
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

func TestResultCategoryValuesMatchPythonIntEnum(t *testing.T) {
	tests := []struct {
		name string
		got  ResultCategory
		want int
	}{
		{name: "success", got: ResultCategorySuccess, want: 0},
		{name: "rate limited", got: ResultCategoryRateLimited, want: 1},
		{name: "auth failure", got: ResultCategoryAuthFailure, want: 2},
		{name: "forbidden", got: ResultCategoryForbidden, want: 3},
		{name: "not found", got: ResultCategoryNotFound, want: 4},
		{name: "upstream 5xx", got: ResultCategoryUpstream5xx, want: 5},
		{name: "transport err", got: ResultCategoryTransportErr, want: 6},
		{name: "unknown", got: ResultCategoryUnknown, want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.got) != tt.want {
				t.Fatalf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestTransportKindValuesMatchPythonIntEnum(t *testing.T) {
	tests := []struct {
		name string
		got  TransportKind
		want int
	}{
		{name: "http sse", got: TransportKindHTTPSSE, want: 0},
		{name: "http json", got: TransportKindHTTPJSON, want: 1},
		{name: "websocket", got: TransportKindWebSocket, want: 2},
		{name: "grpc web", got: TransportKindGRPCWeb, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.got) != tt.want {
				t.Fatalf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestNewReversePlanAppliesPythonDefaults(t *testing.T) {
	plan := NewReversePlan("https://upstream", TransportKindHTTPJSON, []int{0, 2}, 3)

	if plan.Endpoint != "https://upstream" || plan.TransportKind != TransportKindHTTPJSON {
		t.Fatalf("plan endpoint/transport = %#v", plan)
	}
	if len(plan.PoolCandidates) != 2 || plan.PoolCandidates[0] != 0 || plan.PoolCandidates[1] != 2 {
		t.Fatalf("pool candidates = %#v", plan.PoolCandidates)
	}
	if plan.ModeID != 3 || plan.TimeoutS != 120.0 || plan.ContentType != "application/json" {
		t.Fatalf("plan mode/timeout/content = %#v", plan)
	}
	if plan.Origin != "https://grok.com" || plan.Referer != "https://grok.com/" {
		t.Fatalf("plan origin/referer = %#v", plan)
	}
	if plan.Extra == nil || len(plan.Extra) != 0 {
		t.Fatalf("plan extra = %#v, want empty map", plan.Extra)
	}
}

func TestReverseLeaseSetAndResultMatchPythonDefaults(t *testing.T) {
	lease := controlproxy.NewProxyLease("lease-1")
	leaseSet := ReverseLeaseSet{
		AccountIdx:   2,
		AccountToken: "token",
		ProxyLease:   &lease,
	}
	if leaseSet.AccountIdx != 2 || leaseSet.AccountToken != "token" || leaseSet.ProxyLease == nil {
		t.Fatalf("lease set = %#v", leaseSet)
	}

	result := NewReverseResult(ResultCategoryForbidden)
	if result.Category != ResultCategoryForbidden {
		t.Fatalf("category = %v, want %v", result.Category, ResultCategoryForbidden)
	}
	if result.StatusCode != 0 || result.Body != "" || result.Payload != nil || result.Error != "" || result.LatencyMS != 0 {
		t.Fatalf("result defaults = %#v", result)
	}
}
