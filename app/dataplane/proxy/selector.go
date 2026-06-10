package proxy

import controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"

type SelectProxyOptions struct {
	Scope controlproxy.ProxyScope
	Kind  controlproxy.RequestKind
}

func SelectProxy(table ProxyRuntimeTable, options ...SelectProxyOptions) *string {
	_ = options

	if table.EgressMode == controlproxy.EgressModeDirect {
		return nil
	}

	if table.EgressMode == controlproxy.EgressModeSingleProxy {
		if len(table.Nodes) > 0 {
			return table.Nodes[0].ProxyURL
		}
		return nil
	}

	healthy := table.HealthyNodes()
	if len(healthy) == 0 {
		return nil
	}

	best := healthy[0]
	for _, node := range healthy[1:] {
		if node.Inflight < best.Inflight {
			best = node
		}
	}
	return best.ProxyURL
}
