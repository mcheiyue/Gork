package proxy

import (
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

func TestProxyLeaseReExportMatchesControlPlane(t *testing.T) {
	lease := ProxyLease{LeaseID: "lease-1"}
	var controlLease controlproxy.ProxyLease = lease
	if controlLease.LeaseID != "lease-1" {
		t.Fatalf("control lease = %#v", controlLease)
	}

	if lease.HasProxy() {
		t.Fatalf("HasProxy should be available through dataplane alias")
	}
	proxyURL := "http://proxy.local:8080"
	lease.ProxyURL = &proxyURL
	if !lease.HasProxy() {
		t.Fatalf("HasProxy should use control-plane ProxyLease behavior")
	}
}
