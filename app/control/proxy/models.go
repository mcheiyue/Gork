package proxy

import (
	"fmt"
	"strings"
)

type ProxyScope string

const (
	ProxyScopeApp   ProxyScope = "app"
	ProxyScopeAsset ProxyScope = "asset"
)

type RequestKind string

const (
	RequestKindHTTP      RequestKind = "http"
	RequestKindWebSocket RequestKind = "websocket"
	RequestKindGRPC      RequestKind = "grpc"
)

type EgressMode string

const (
	EgressModeDirect      EgressMode = "direct"
	EgressModeSingleProxy EgressMode = "single_proxy"
	EgressModeProxyPool   EgressMode = "proxy_pool"
)

type ClearanceMode string

const (
	ClearanceModeNone         ClearanceMode = "none"
	ClearanceModeManual       ClearanceMode = "manual"
	ClearanceModeFlareSolverr ClearanceMode = "flaresolverr"
)

func ParseClearanceMode(value any) (ClearanceMode, error) {
	switch typed := value.(type) {
	case nil:
		return ClearanceModeNone, nil
	case ClearanceMode:
		return validateClearanceMode(typed)
	case string:
		return parseClearanceModeString(typed)
	default:
		return parseClearanceModeString(fmt.Sprint(typed))
	}
}

func parseClearanceModeString(value string) (ClearanceMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ClearanceModeNone, nil
	}
	return validateClearanceMode(ClearanceMode(normalized))
}

func validateClearanceMode(mode ClearanceMode) (ClearanceMode, error) {
	switch mode {
	case ClearanceModeNone, ClearanceModeManual, ClearanceModeFlareSolverr:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid ClearanceMode: %q", mode)
	}
}

type EgressNodeState int

const (
	EgressNodeHealthy EgressNodeState = iota
	EgressNodeDegraded
	EgressNodeUnhealthy
)

type ClearanceBundleState int

const (
	ClearanceBundleValid ClearanceBundleState = iota
	ClearanceBundleStale
	ClearanceBundleInvalid
)

type ProxyFeedbackKind string

const (
	ProxyFeedbackSuccess        ProxyFeedbackKind = "success"
	ProxyFeedbackChallenge      ProxyFeedbackKind = "challenge"
	ProxyFeedbackUnauthorized   ProxyFeedbackKind = "unauthorized"
	ProxyFeedbackForbidden      ProxyFeedbackKind = "forbidden"
	ProxyFeedbackRateLimited    ProxyFeedbackKind = "rate_limited"
	ProxyFeedbackUpstream5xx    ProxyFeedbackKind = "upstream_5xx"
	ProxyFeedbackTransportError ProxyFeedbackKind = "transport_error"
)

type EgressNode struct {
	NodeID   string          `json:"node_id"`
	ProxyURL *string         `json:"proxy_url"`
	Scope    ProxyScope      `json:"scope"`
	State    EgressNodeState `json:"state"`
	Health   float64         `json:"health"`
	Inflight int             `json:"inflight"`
	LastUsed *int64          `json:"last_used"`
}

func NewEgressNode(nodeID string) EgressNode {
	return EgressNode{
		NodeID: nodeID,
		Scope:  ProxyScopeApp,
		State:  EgressNodeHealthy,
		Health: 1.0,
	}
}

type ClearanceBundle struct {
	BundleID      string               `json:"bundle_id"`
	CFCookies     string               `json:"cf_cookies"`
	UserAgent     string               `json:"user_agent"`
	State         ClearanceBundleState `json:"state"`
	AffinityKey   string               `json:"affinity_key"`
	ClearanceHost string               `json:"clearance_host"`
	LastRefreshAt *int64               `json:"last_refresh_at"`
}

func NewClearanceBundle(bundleID string) ClearanceBundle {
	return ClearanceBundle{
		BundleID:      bundleID,
		State:         ClearanceBundleValid,
		ClearanceHost: "grok.com",
	}
}

type ProxyLease struct {
	LeaseID       string      `json:"lease_id"`
	ProxyURL      *string     `json:"proxy_url"`
	CFCookies     string      `json:"cf_cookies"`
	UserAgent     string      `json:"user_agent"`
	ClearanceHost string      `json:"clearance_host"`
	Scope         ProxyScope  `json:"scope"`
	Kind          RequestKind `json:"kind"`
	AcquiredAt    int64       `json:"acquired_at"`
}

func NewProxyLease(leaseID string) ProxyLease {
	return ProxyLease{
		LeaseID:       leaseID,
		ClearanceHost: "grok.com",
		Scope:         ProxyScopeApp,
		Kind:          RequestKindHTTP,
	}
}

func (l ProxyLease) HasProxy() bool {
	return l.ProxyURL != nil && *l.ProxyURL != ""
}

type ProxyFeedback struct {
	Kind         ProxyFeedbackKind `json:"kind"`
	StatusCode   *int              `json:"status_code"`
	Reason       string            `json:"reason"`
	RetryAfterMS *int64            `json:"retry_after_ms"`
}

func NewProxyFeedback(kind ProxyFeedbackKind) ProxyFeedback {
	return ProxyFeedback{Kind: kind}
}
