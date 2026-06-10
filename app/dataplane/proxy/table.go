package proxy

import controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"

type BundleKey struct {
	AffinityKey   string
	ClearanceHost string
}

type ProxyRuntimeTable struct {
	EgressMode    controlproxy.EgressMode
	ClearanceMode controlproxy.ClearanceMode
	Nodes         []controlproxy.EgressNode
	Bundles       map[BundleKey]controlproxy.ClearanceBundle
}

type ProxyDirectorySnapshot interface {
	EgressMode() controlproxy.EgressMode
	ClearanceMode() controlproxy.ClearanceMode
	Nodes() []controlproxy.EgressNode
	Bundles() map[BundleKey]controlproxy.ClearanceBundle
}

func NewProxyRuntimeTable() ProxyRuntimeTable {
	return ProxyRuntimeTable{
		EgressMode:    controlproxy.EgressModeDirect,
		ClearanceMode: controlproxy.ClearanceModeNone,
		Nodes:         []controlproxy.EgressNode{},
		Bundles:       map[BundleKey]controlproxy.ClearanceBundle{},
	}
}

func (t ProxyRuntimeTable) NodeCount() int {
	return len(t.Nodes)
}

func (t ProxyRuntimeTable) HasNodes() bool {
	return len(t.Nodes) > 0
}

func (t ProxyRuntimeTable) HealthyNodes() []controlproxy.EgressNode {
	healthy := make([]controlproxy.EgressNode, 0, len(t.Nodes))
	for _, node := range t.Nodes {
		if node.State == controlproxy.EgressNodeHealthy {
			healthy = append(healthy, node)
		}
	}
	return healthy
}

func SnapshotFromDirectory(directory ProxyDirectorySnapshot) ProxyRuntimeTable {
	return ProxyRuntimeTable{
		EgressMode:    directory.EgressMode(),
		ClearanceMode: directory.ClearanceMode(),
		Nodes:         directory.Nodes(),
		Bundles:       directory.Bundles(),
	}
}
