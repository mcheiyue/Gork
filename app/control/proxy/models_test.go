package proxy

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProxyEnumValuesMatchPython(t *testing.T) {
	if ProxyScopeApp != "app" || ProxyScopeAsset != "asset" {
		t.Fatalf("proxy scope values changed")
	}
	if RequestKindHTTP != "http" || RequestKindWebSocket != "websocket" || RequestKindGRPC != "grpc" {
		t.Fatalf("request kind values changed")
	}
	if EgressModeDirect != "direct" || EgressModeSingleProxy != "single_proxy" || EgressModeProxyPool != "proxy_pool" {
		t.Fatalf("egress mode values changed")
	}
	if ClearanceModeNone != "none" || ClearanceModeManual != "manual" || ClearanceModeFlareSolverr != "flaresolverr" {
		t.Fatalf("clearance mode values changed")
	}
	if EgressNodeHealthy != 0 || EgressNodeDegraded != 1 || EgressNodeUnhealthy != 2 {
		t.Fatalf("egress node state values changed")
	}
	if ClearanceBundleValid != 0 || ClearanceBundleStale != 1 || ClearanceBundleInvalid != 2 {
		t.Fatalf("clearance bundle state values changed")
	}
}

func TestProxyFeedbackKindValuesMatchPython(t *testing.T) {
	cases := map[ProxyFeedbackKind]string{
		ProxyFeedbackSuccess:        "success",
		ProxyFeedbackChallenge:      "challenge",
		ProxyFeedbackUnauthorized:   "unauthorized",
		ProxyFeedbackForbidden:      "forbidden",
		ProxyFeedbackRateLimited:    "rate_limited",
		ProxyFeedbackUpstream5xx:    "upstream_5xx",
		ProxyFeedbackTransportError: "transport_error",
	}
	for kind, want := range cases {
		if string(kind) != want {
			t.Fatalf("feedback kind = %q, want %q", kind, want)
		}
	}
}

func TestParseClearanceModeMatchesPython(t *testing.T) {
	cases := []struct {
		value any
		want  ClearanceMode
	}{
		{nil, ClearanceModeNone},
		{"", ClearanceModeNone},
		{"  MANUAL  ", ClearanceModeManual},
		{"FlareSolverr", ClearanceModeFlareSolverr},
		{ClearanceModeManual, ClearanceModeManual},
	}
	for _, tt := range cases {
		got, err := ParseClearanceMode(tt.value)
		if err != nil {
			t.Fatalf("ParseClearanceMode(%#v) returned error: %v", tt.value, err)
		}
		if got != tt.want {
			t.Fatalf("ParseClearanceMode(%#v) = %q, want %q", tt.value, got, tt.want)
		}
	}

	if _, err := ParseClearanceMode("invalid"); err == nil {
		t.Fatalf("ParseClearanceMode should reject unknown modes")
	}
}

func TestProxyModelDefaultsMatchPython(t *testing.T) {
	node := NewEgressNode("node-1")
	if node.NodeID != "node-1" || node.ProxyURL != nil || node.Scope != ProxyScopeApp ||
		node.State != EgressNodeHealthy || node.Health != 1.0 || node.Inflight != 0 || node.LastUsed != nil {
		t.Fatalf("NewEgressNode defaults = %#v", node)
	}

	bundle := NewClearanceBundle("bundle-1")
	if bundle.BundleID != "bundle-1" || bundle.CFCookies != "" || bundle.UserAgent != "" ||
		bundle.State != ClearanceBundleValid || bundle.AffinityKey != "" ||
		bundle.ClearanceHost != "grok.com" || bundle.LastRefreshAt != nil {
		t.Fatalf("NewClearanceBundle defaults = %#v", bundle)
	}

	lease := NewProxyLease("lease-1")
	if lease.LeaseID != "lease-1" || lease.ProxyURL != nil || lease.CFCookies != "" ||
		lease.UserAgent != "" || lease.ClearanceHost != "grok.com" ||
		lease.Scope != ProxyScopeApp || lease.Kind != RequestKindHTTP || lease.AcquiredAt != 0 {
		t.Fatalf("NewProxyLease defaults = %#v", lease)
	}
	if lease.HasProxy() {
		t.Fatalf("HasProxy should be false when ProxyURL is nil")
	}
	emptyProxyURL := ""
	lease.ProxyURL = &emptyProxyURL
	if lease.HasProxy() {
		t.Fatalf("HasProxy should be false when ProxyURL is empty")
	}
	proxyURL := "http://proxy.local:8080"
	lease.ProxyURL = &proxyURL
	if !lease.HasProxy() {
		t.Fatalf("HasProxy should be true when ProxyURL is non-empty")
	}

	feedback := NewProxyFeedback(ProxyFeedbackRateLimited)
	if feedback.Kind != ProxyFeedbackRateLimited || feedback.StatusCode != nil ||
		feedback.Reason != "" || feedback.RetryAfterMS != nil {
		t.Fatalf("NewProxyFeedback defaults = %#v", feedback)
	}
}

func TestProxyModelJSONShapeMatchesPythonModelDump(t *testing.T) {
	assertJSONMap(t, NewEgressNode("node-1"), map[string]any{
		"node_id":   "node-1",
		"proxy_url": nil,
		"scope":     "app",
		"state":     float64(0),
		"health":    float64(1),
		"inflight":  float64(0),
		"last_used": nil,
	})
	assertJSONMap(t, NewClearanceBundle("bundle-1"), map[string]any{
		"bundle_id":       "bundle-1",
		"cf_cookies":      "",
		"user_agent":      "",
		"state":           float64(0),
		"affinity_key":    "",
		"clearance_host":  "grok.com",
		"last_refresh_at": nil,
	})
	assertJSONMap(t, NewProxyLease("lease-1"), map[string]any{
		"lease_id":       "lease-1",
		"proxy_url":      nil,
		"cf_cookies":     "",
		"user_agent":     "",
		"clearance_host": "grok.com",
		"scope":          "app",
		"kind":           "http",
		"acquired_at":    float64(0),
	})
	assertJSONMap(t, NewProxyFeedback(ProxyFeedbackRateLimited), map[string]any{
		"kind":           "rate_limited",
		"status_code":    nil,
		"reason":         "",
		"retry_after_ms": nil,
	})
}

func assertJSONMap(t *testing.T, value any, want map[string]any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %T JSON %s: %v", value, raw, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%T JSON = %#v, want %#v", value, got, want)
	}
}
