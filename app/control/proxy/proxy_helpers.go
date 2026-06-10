package proxy

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type affinityItem struct {
	affinity string
	proxyURL string
}

type refreshTarget struct {
	proxyURL string
	origin   string
}

func defaultAcquireOptions(options ...AcquireOptions) AcquireOptions {
	option := AcquireOptions{Scope: ProxyScopeApp, Kind: RequestKindHTTP}
	if len(options) > 0 {
		option = options[0]
		if option.Scope == "" {
			option.Scope = ProxyScopeApp
		}
		if option.Kind == "" {
			option.Kind = RequestKindHTTP
		}
	}
	return option
}

func buildEgressNodes(mode EgressMode, baseURL, resourceURL string, basePool, resourcePool []string) ([]EgressNode, []EgressNode) {
	var nodes []EgressNode
	var resourceNodes []EgressNode
	if mode == EgressModeSingleProxy {
		if baseURL != "" {
			nodes = append(nodes, newEgressNode("single", baseURL))
		}
		if resourceURL != "" {
			resourceNodes = append(resourceNodes, newEgressNode("res-single", resourceURL))
		}
	}
	if mode == EgressModeProxyPool {
		for i, proxyURL := range basePool {
			nodes = append(nodes, newEgressNode(fmt.Sprintf("pool-%d", i), proxyURL))
		}
		for i, proxyURL := range resourcePool {
			resourceNodes = append(resourceNodes, newEgressNode(fmt.Sprintf("res-pool-%d", i), proxyURL))
		}
	}
	return nodes, resourceNodes
}

func newEgressNode(nodeID, proxyURL string) EgressNode {
	node := NewEgressNode(nodeID)
	node.ProxyURL = stringPtr(proxyURL)
	return node
}

func affinityItems(nodes []EgressNode) []affinityItem {
	if len(nodes) == 0 {
		return []affinityItem{{affinity: "direct"}}
	}
	items := make([]affinityItem, 0, len(nodes))
	for _, node := range nodes {
		proxyURL := ""
		affinity := "direct"
		if node.ProxyURL != nil && *node.ProxyURL != "" {
			proxyURL = *node.ProxyURL
			affinity = *node.ProxyURL
		}
		items = append(items, affinityItem{affinity: affinity, proxyURL: proxyURL})
	}
	return items
}

func validAffinities(nodes, resourceNodes []EgressNode) map[string]bool {
	valid := map[string]bool{}
	for _, node := range append(copyNodes(nodes), resourceNodes...) {
		if node.ProxyURL != nil && *node.ProxyURL != "" {
			valid[*node.ProxyURL] = true
		}
	}
	if len(valid) == 0 {
		valid["direct"] = true
	}
	return valid
}

func invalidateMatchingBundles(current map[BundleKey]ClearanceBundle, valid map[string]bool) map[BundleKey]ClearanceBundle {
	next := map[BundleKey]ClearanceBundle{}
	for key, bundle := range current {
		if valid[key.Affinity] {
			bundle.State = ClearanceBundleInvalid
			next[key] = bundle
		}
	}
	return next
}

func filterRefreshEvents(current map[BundleKey]*refreshEvent, valid map[string]bool) map[BundleKey]*refreshEvent {
	next := map[BundleKey]*refreshEvent{}
	for key, event := range current {
		if valid[key.Affinity] {
			next[key] = event
		}
	}
	return next
}

func parseEgressMode(value string) (EgressMode, error) {
	mode := EgressMode(strings.ToLower(strings.TrimSpace(value)))
	if mode == "" {
		mode = EgressModeDirect
	}
	switch mode {
	case EgressModeDirect, EgressModeSingleProxy, EgressModeProxyPool:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid EgressMode: %q", mode)
	}
}

func clearanceHost(origin string) string {
	if origin == "" {
		origin = defaultClearanceOrigin
	}
	parsed, err := url.Parse(origin)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(parsed.Hostname())
	}
	return "grok.com"
}

func leaseAffinity(lease ProxyLease) string {
	if lease.ProxyURL != nil && *lease.ProxyURL != "" {
		return *lease.ProxyURL
	}
	return "direct"
}

func leaseClearanceHost(lease ProxyLease) string {
	if lease.ClearanceHost != "" {
		return lease.ClearanceHost
	}
	return "grok.com"
}

func rotatesPool(kind ProxyFeedbackKind) bool {
	return kind == ProxyFeedbackChallenge ||
		kind == ProxyFeedbackUnauthorized ||
		kind == ProxyFeedbackForbidden ||
		kind == ProxyFeedbackTransportError
}

func copyNodes(nodes []EgressNode) []EgressNode {
	out := make([]EgressNode, len(nodes))
	for i, node := range nodes {
		out[i] = node
		out[i].ProxyURL = cloneStringPtr(node.ProxyURL)
	}
	return out
}

func stringPtr(value string) *string {
	return &value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPtr(*value)
}

type configManualClearanceProvider struct {
	config StringConfig
}

func (p configManualClearanceProvider) BuildBundle(affinityKey string, clearanceHost ...string) (ClearanceBundle, bool, error) {
	modeValue := "none"
	if p.config != nil {
		modeValue = p.config.GetString("proxy.clearance.mode", "none")
	}
	mode, err := ParseClearanceMode(modeValue)
	if err != nil {
		return ClearanceBundle{}, false, err
	}
	if mode != ClearanceModeManual {
		return ClearanceBundle{}, false, nil
	}
	host := "grok.com"
	if len(clearanceHost) > 0 && clearanceHost[0] != "" {
		host = clearanceHost[0]
	}
	clearance := ResolveClearanceConfig(p.config)
	bundle := NewClearanceBundle(fmt.Sprintf("manual:%s@%s", affinityKey, host))
	bundle.CFCookies = clearance.CFCookies
	bundle.UserAgent = clearance.UserAgent
	bundle.AffinityKey = affinityKey
	bundle.ClearanceHost = host
	return bundle, true, nil
}

type noopFlareClearanceProvider struct{}

func (noopFlareClearanceProvider) RefreshBundle(context.Context, string, string, ...string) (ClearanceBundle, bool, error) {
	return ClearanceBundle{}, false, nil
}
