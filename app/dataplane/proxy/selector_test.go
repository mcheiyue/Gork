package proxy

import (
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

func TestSelectProxyReturnsNilForDirectMode(t *testing.T) {
	proxyURL := "http://proxy.local:8080"
	node := controlproxy.NewEgressNode("node-1")
	node.ProxyURL = &proxyURL
	table := NewProxyRuntimeTable()
	table.EgressMode = controlproxy.EgressModeDirect
	table.Nodes = []controlproxy.EgressNode{node}

	if got := SelectProxy(table); got != nil {
		t.Fatalf("SelectProxy direct = %q, want nil", *got)
	}
}

func TestSelectProxySingleProxyReturnsFirstNodeURL(t *testing.T) {
	firstURL := "http://first.local:8080"
	secondURL := "http://second.local:8080"
	first := controlproxy.NewEgressNode("first")
	first.ProxyURL = &firstURL
	second := controlproxy.NewEgressNode("second")
	second.ProxyURL = &secondURL
	table := NewProxyRuntimeTable()
	table.EgressMode = controlproxy.EgressModeSingleProxy
	table.Nodes = []controlproxy.EgressNode{first, second}

	got := SelectProxy(table, SelectProxyOptions{
		Scope: controlproxy.ProxyScopeAsset,
		Kind:  controlproxy.RequestKindWebSocket,
	})
	if got == nil || *got != firstURL {
		t.Fatalf("SelectProxy single proxy = %v, want first url", got)
	}
}

func TestSelectProxyPoolPicksHealthyLowestInflight(t *testing.T) {
	busyURL := "http://busy.local:8080"
	bestURL := "http://best.local:8080"
	unhealthyURL := "http://unhealthy.local:8080"
	busy := controlproxy.NewEgressNode("busy")
	busy.ProxyURL = &busyURL
	busy.Inflight = 3
	best := controlproxy.NewEgressNode("best")
	best.ProxyURL = &bestURL
	best.Inflight = 1
	unhealthy := controlproxy.NewEgressNode("unhealthy")
	unhealthy.ProxyURL = &unhealthyURL
	unhealthy.State = controlproxy.EgressNodeUnhealthy

	table := NewProxyRuntimeTable()
	table.EgressMode = controlproxy.EgressModeProxyPool
	table.Nodes = []controlproxy.EgressNode{busy, unhealthy, best}

	got := SelectProxy(table)
	if got == nil || *got != bestURL {
		t.Fatalf("SelectProxy pool = %v, want best url", got)
	}
}

func TestSelectProxyPoolReturnsNilWithoutHealthyNodes(t *testing.T) {
	node := controlproxy.NewEgressNode("node-1")
	node.State = controlproxy.EgressNodeDegraded
	table := NewProxyRuntimeTable()
	table.EgressMode = controlproxy.EgressModeProxyPool
	table.Nodes = []controlproxy.EgressNode{node}

	if got := SelectProxy(table); got != nil {
		t.Fatalf("SelectProxy without healthy nodes = %v, want nil", got)
	}
}
