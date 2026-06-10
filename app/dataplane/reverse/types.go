package reverse

import controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"

type ResultCategory int

const (
	ResultCategorySuccess ResultCategory = iota
	ResultCategoryRateLimited
	ResultCategoryAuthFailure
	ResultCategoryForbidden
	ResultCategoryNotFound
	ResultCategoryUpstream5xx
	ResultCategoryTransportErr
	ResultCategoryUnknown
)

type TransportKind int

const (
	TransportKindHTTPSSE TransportKind = iota
	TransportKindHTTPJSON
	TransportKindWebSocket
	TransportKindGRPCWeb
)

func (c ResultCategory) Name() string {
	switch c {
	case ResultCategorySuccess:
		return "SUCCESS"
	case ResultCategoryRateLimited:
		return "RATE_LIMITED"
	case ResultCategoryAuthFailure:
		return "AUTH_FAILURE"
	case ResultCategoryForbidden:
		return "FORBIDDEN"
	case ResultCategoryNotFound:
		return "NOT_FOUND"
	case ResultCategoryUpstream5xx:
		return "UPSTREAM_5XX"
	case ResultCategoryTransportErr:
		return "TRANSPORT_ERR"
	case ResultCategoryUnknown:
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

func (c ResultCategory) String() string {
	return c.Name()
}

type ReversePlan struct {
	Endpoint       string
	TransportKind  TransportKind
	PoolCandidates []int
	ModeID         int
	TimeoutS       float64
	ContentType    string
	Origin         string
	Referer        string
	Extra          map[string]any
}

func NewReversePlan(endpoint string, transportKind TransportKind, poolCandidates []int, modeID int) ReversePlan {
	return ReversePlan{
		Endpoint:       endpoint,
		TransportKind:  transportKind,
		PoolCandidates: append([]int(nil), poolCandidates...),
		ModeID:         modeID,
		TimeoutS:       120.0,
		ContentType:    "application/json",
		Origin:         "https://grok.com",
		Referer:        "https://grok.com/",
		Extra:          map[string]any{},
	}
}

type ReverseLeaseSet struct {
	AccountIdx   int
	AccountToken string
	ProxyLease   *controlproxy.ProxyLease
}

type ReverseResult struct {
	Category   ResultCategory
	StatusCode int
	Body       string
	Payload    any
	Error      string
	LatencyMS  int
}

func NewReverseResult(category ResultCategory) ReverseResult {
	return ReverseResult{Category: category}
}
