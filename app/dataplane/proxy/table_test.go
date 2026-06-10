package proxy

import (
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

type fakeDirectorySnapshot struct {
	egressMode    controlproxy.EgressMode
	clearanceMode controlproxy.ClearanceMode
	nodes         []controlproxy.EgressNode
	bundles       map[BundleKey]controlproxy.ClearanceBundle
}

func (f fakeDirectorySnapshot) EgressMode() controlproxy.EgressMode       { return f.egressMode }
func (f fakeDirectorySnapshot) ClearanceMode() controlproxy.ClearanceMode { return f.clearanceMode }
func (f fakeDirectorySnapshot) Nodes() []controlproxy.EgressNode          { return f.nodes }
func (f fakeDirectorySnapshot) Bundles() map[BundleKey]controlproxy.ClearanceBundle {
	return f.bundles
}

func TestProxyRuntimeTableDefaultsMatchPython(t *testing.T) {
	table := NewProxyRuntimeTable()
	if table.EgressMode != controlproxy.EgressModeDirect ||
		table.ClearanceMode != controlproxy.ClearanceModeNone ||
		table.NodeCount() != 0 ||
		table.HasNodes() ||
		len(table.Nodes) != 0 ||
		len(table.Bundles) != 0 {
		t.Fatalf("default table = %#v", table)
	}
}

func TestProxyRuntimeTableHealthyNodesMatchPython(t *testing.T) {
	healthy := controlproxy.NewEgressNode("healthy")
	degraded := controlproxy.NewEgressNode("degraded")
	degraded.State = controlproxy.EgressNodeDegraded
	table := NewProxyRuntimeTable()
	table.Nodes = []controlproxy.EgressNode{healthy, degraded}

	if table.NodeCount() != 2 || !table.HasNodes() {
		t.Fatalf("node count/has nodes mismatch: %#v", table)
	}
	got := table.HealthyNodes()
	if len(got) != 1 || got[0].NodeID != "healthy" {
		t.Fatalf("HealthyNodes = %#v", got)
	}
}

func TestSnapshotFromDirectoryMatchesPython(t *testing.T) {
	node := controlproxy.NewEgressNode("node-1")
	key := BundleKey{AffinityKey: "node-1", ClearanceHost: "grok.com"}
	directory := fakeDirectorySnapshot{
		egressMode:    controlproxy.EgressModeProxyPool,
		clearanceMode: controlproxy.ClearanceModeManual,
		nodes:         []controlproxy.EgressNode{node},
		bundles:       map[BundleKey]controlproxy.ClearanceBundle{key: controlproxy.NewClearanceBundle("bundle-1")},
	}

	table := SnapshotFromDirectory(directory)
	if table.EgressMode != controlproxy.EgressModeProxyPool ||
		table.ClearanceMode != controlproxy.ClearanceModeManual ||
		table.NodeCount() != 1 ||
		table.Nodes[0].NodeID != "node-1" ||
		table.Bundles[key].BundleID != "bundle-1" {
		t.Fatalf("SnapshotFromDirectory = %#v", table)
	}
}
