package reverse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

type AccountLease struct {
	Idx   int
	Token string
}

type AccountFeedbackKind string

const (
	AccountFeedbackSuccess      AccountFeedbackKind = "success"
	AccountFeedbackUnauthorized AccountFeedbackKind = "unauthorized"
	AccountFeedbackForbidden    AccountFeedbackKind = "forbidden"
	AccountFeedbackRateLimited  AccountFeedbackKind = "rate_limited"
	AccountFeedbackServerError  AccountFeedbackKind = "server_error"
)

type AccountDirectory interface {
	Reserve(ctx context.Context, poolCandidates []int, modeID int) (*AccountLease, error)
	Release(ctx context.Context, lease AccountLease) error
	Feedback(ctx context.Context, token string, kind AccountFeedbackKind, modeID int) error
}

type ProxyRuntime interface {
	Acquire(ctx context.Context) (*controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
}

type PayloadBuilder func(plan ReversePlan, accountToken string, request map[string]any) ([]byte, error)

type ReverseTransport func(ctx context.Context, request TransportRequest) (any, error)

type TransportRequest struct {
	Endpoint     string
	AccountToken string
	Payload      []byte
	ProxyLease   *controlproxy.ProxyLease
	TimeoutS     float64
	ContentType  string
	Origin       string
	Referer      string
}

type ExecutorOptions struct {
	Planner          func(spec controlmodel.ModelSpec, request map[string]any) ReversePlan
	AccountDirectory AccountDirectory
	ProxyRuntime     ProxyRuntime
	PayloadBuilder   PayloadBuilder
	Transport        ReverseTransport
	Clock            func() int64
}

type UpstreamError struct {
	Status  int
	Details map[string]string
	Message string
}

func (e UpstreamError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("upstream status %d", e.Status)
}

func Execute(ctx context.Context, spec controlmodel.ModelSpec, request map[string]any, options ...ExecutorOptions) ReverseResult {
	executor := executorOptions(options...)
	t0 := executor.Clock()
	plan := executor.Planner(spec, request)
	lease, err := reserveAccount(ctx, executor.AccountDirectory, plan)
	if err != nil {
		return timedResult(transportErrorResult(err), t0, executor.Clock())
	}
	if lease == nil {
		return timedResult(ReverseResult{
			Category: ResultCategoryRateLimited,
			Error:    "No available accounts",
		}, t0, executor.Clock())
	}
	proxyLease, err := acquireProxy(ctx, executor.ProxyRuntime)
	leases := ReverseLeaseSet{AccountIdx: lease.Idx, AccountToken: lease.Token, ProxyLease: proxyLease}
	if err != nil {
		result := timedResult(transportErrorResult(err), t0, executor.Clock())
		applyFeedbackAndRelease(ctx, executor, plan, leases, result, *lease)
		return result
	}
	result := executeTransport(ctx, executor, plan, leases, request)
	result = timedResult(result, t0, executor.Clock())
	applyFeedbackAndRelease(ctx, executor, plan, leases, result, *lease)
	return result
}

func executorOptions(options ...ExecutorOptions) ExecutorOptions {
	option := ExecutorOptions{
		Planner: func(spec controlmodel.ModelSpec, request map[string]any) ReversePlan {
			return BuildPlan(spec, BuildPlanOptions{Request: request})
		},
		Clock: platformruntime.NowMS,
	}
	if len(options) > 0 {
		option = options[0]
	}
	if option.Planner == nil {
		option.Planner = func(spec controlmodel.ModelSpec, request map[string]any) ReversePlan {
			return BuildPlan(spec, BuildPlanOptions{Request: request})
		}
	}
	if option.Clock == nil {
		option.Clock = platformruntime.NowMS
	}
	return option
}

func reserveAccount(ctx context.Context, directory AccountDirectory, plan ReversePlan) (*AccountLease, error) {
	if directory == nil {
		return nil, errors.New("reverse account directory is not configured")
	}
	return directory.Reserve(ctx, plan.PoolCandidates, plan.ModeID)
}

func acquireProxy(ctx context.Context, runtime ProxyRuntime) (*controlproxy.ProxyLease, error) {
	if runtime == nil {
		return nil, errors.New("reverse proxy runtime is not configured")
	}
	return runtime.Acquire(ctx)
}

func executeTransport(ctx context.Context, executor ExecutorOptions, plan ReversePlan, leases ReverseLeaseSet, request map[string]any) ReverseResult {
	payload, err := buildPayload(executor.PayloadBuilder, plan, leases.AccountToken, request)
	if err != nil {
		return transportErrorResult(err)
	}
	if executor.Transport == nil {
		return transportErrorResult(errors.New("reverse transport is not configured"))
	}
	raw, err := executor.Transport(ctx, transportRequest(plan, leases, payload))
	if err != nil {
		return resultFromTransportError(err)
	}
	return ReverseResult{Category: ClassifyResult(200, ""), StatusCode: 200, Payload: raw}
}

func buildPayload(builder PayloadBuilder, plan ReversePlan, accountToken string, request map[string]any) ([]byte, error) {
	if builder != nil {
		return builder(plan, accountToken, request)
	}
	return json.Marshal(request)
}

func transportRequest(plan ReversePlan, leases ReverseLeaseSet, payload []byte) TransportRequest {
	return TransportRequest{
		Endpoint:     plan.Endpoint,
		AccountToken: leases.AccountToken,
		Payload:      payload,
		ProxyLease:   leases.ProxyLease,
		TimeoutS:     plan.TimeoutS,
		ContentType:  plan.ContentType,
		Origin:       plan.Origin,
		Referer:      plan.Referer,
	}
}

func resultFromTransportError(err error) ReverseResult {
	if upstream, ok := asUpstreamError(err); ok {
		body := upstream.Details["body"]
		return ReverseResult{
			Category:   ClassifyResult(upstream.Status, body),
			StatusCode: upstream.Status,
			Body:       body,
			Error:      upstream.Error(),
		}
	}
	return transportErrorResult(err)
}

func asUpstreamError(err error) (*UpstreamError, bool) {
	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		return upstream, true
	}
	var upstreamValue UpstreamError
	if errors.As(err, &upstreamValue) {
		return &upstreamValue, true
	}
	return nil, false
}

func transportErrorResult(err error) ReverseResult {
	return ReverseResult{Category: ResultCategoryTransportErr, Error: err.Error()}
}

func timedResult(result ReverseResult, startMS int64, endMS int64) ReverseResult {
	result.LatencyMS = int(endMS - startMS)
	return result
}

func applyFeedbackAndRelease(
	ctx context.Context,
	executor ExecutorOptions,
	plan ReversePlan,
	leases ReverseLeaseSet,
	result ReverseResult,
	accountLease AccountLease,
) {
	if executor.AccountDirectory == nil {
		return
	}
	if err := executor.AccountDirectory.Release(ctx, accountLease); err != nil {
		return
	}
	if err := applyAccountFeedback(ctx, executor.AccountDirectory, plan, leases, result); err != nil {
		return
	}
	applyProxyFeedback(ctx, executor.ProxyRuntime, leases, result)
}

func applyAccountFeedback(
	ctx context.Context,
	directory AccountDirectory,
	plan ReversePlan,
	leases ReverseLeaseSet,
	result ReverseResult,
) error {
	kind, ok := accountFeedbackKindForCategory(result.Category)
	if !ok {
		return nil
	}
	return directory.Feedback(ctx, leases.AccountToken, kind, plan.ModeID)
}

func applyProxyFeedback(ctx context.Context, runtime ProxyRuntime, leases ReverseLeaseSet, result ReverseResult) {
	if runtime == nil || leases.ProxyLease == nil {
		return
	}
	_ = runtime.Feedback(ctx, *leases.ProxyLease, BuildProxyFeedback(result))
}

func accountFeedbackKindForCategory(category ResultCategory) (AccountFeedbackKind, bool) {
	switch category {
	case ResultCategorySuccess:
		return AccountFeedbackSuccess, true
	case ResultCategoryRateLimited:
		return AccountFeedbackRateLimited, true
	case ResultCategoryAuthFailure:
		return AccountFeedbackUnauthorized, true
	case ResultCategoryForbidden:
		return AccountFeedbackForbidden, true
	case ResultCategoryUpstream5xx, ResultCategoryTransportErr, ResultCategoryUnknown:
		return AccountFeedbackServerError, true
	default:
		return "", false
	}
}
