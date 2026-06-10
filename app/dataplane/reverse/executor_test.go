package reverse

import (
	"context"
	"errors"
	"reflect"
	"testing"

	controlmodel "github.com/jiujiu532/grok2api/app/control/model"
	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

func TestExecuteReturnsRateLimitedWhenNoAccountLease(t *testing.T) {
	directory := &fakeAccountDirectory{}
	proxyRuntime := &fakeProxyRuntime{}
	transportCalled := false

	result := Execute(context.Background(), executorSpec(), map[string]any{"prompt": "hi"}, ExecutorOptions{
		Planner:          fixedPlanner(executorPlan()),
		AccountDirectory: directory,
		ProxyRuntime:     proxyRuntime,
		Transport: func(context.Context, TransportRequest) (any, error) {
			transportCalled = true
			return nil, nil
		},
		Clock: sequenceClock(1000, 1042),
	})

	if result.Category != ResultCategoryRateLimited {
		t.Fatalf("category = %v, want %v", result.Category, ResultCategoryRateLimited)
	}
	if result.Error != "No available accounts" || result.LatencyMS != 42 {
		t.Fatalf("rate limited result = %#v", result)
	}
	if !reflect.DeepEqual(directory.reservePool, []int{7, 3}) || directory.reserveMode != 2 {
		t.Fatalf("reserve call = %#v/%d", directory.reservePool, directory.reserveMode)
	}
	if proxyRuntime.acquireCount != 0 || transportCalled {
		t.Fatalf("proxy acquire/transport should not run: acquire=%d transport=%v", proxyRuntime.acquireCount, transportCalled)
	}
}

func TestExecuteUsesPayloadBuilderTransportAndFeedback(t *testing.T) {
	accountLease := AccountLease{Idx: 11, Token: "acct-token"}
	proxyLease := controlproxy.NewProxyLease("proxy-lease")
	directory := &fakeAccountDirectory{lease: &accountLease}
	proxyRuntime := &fakeProxyRuntime{lease: &proxyLease}
	payload := []byte("custom-payload")
	raw := map[string]any{"ok": true}
	var captured TransportRequest

	result := Execute(context.Background(), executorSpec(), map[string]any{"prompt": "hi"}, ExecutorOptions{
		Planner:          fixedPlanner(executorPlan()),
		AccountDirectory: directory,
		ProxyRuntime:     proxyRuntime,
		PayloadBuilder: func(plan ReversePlan, token string, request map[string]any) ([]byte, error) {
			if plan.Endpoint != "https://upstream.test/reverse" || token != "acct-token" || request["prompt"] != "hi" {
				t.Fatalf("payload builder args = %#v/%q/%#v", plan, token, request)
			}
			return payload, nil
		},
		Transport: func(_ context.Context, request TransportRequest) (any, error) {
			captured = request
			return raw, nil
		},
		Clock: sequenceClock(1000, 1123),
	})

	if result.Category != ResultCategorySuccess || result.StatusCode != 200 || result.LatencyMS != 123 {
		t.Fatalf("success result = %#v", result)
	}
	if !reflect.DeepEqual(result.Payload, raw) {
		t.Fatalf("payload = %#v, want %#v", result.Payload, raw)
	}
	assertTransportRequest(t, captured, payload)
	if directory.releaseCount != 1 || directory.released.Token != "acct-token" {
		t.Fatalf("release call = %d/%#v", directory.releaseCount, directory.released)
	}
	if len(directory.feedbacks) != 1 || directory.feedbacks[0].kind != AccountFeedbackSuccess || directory.feedbacks[0].modeID != 2 {
		t.Fatalf("account feedbacks = %#v", directory.feedbacks)
	}
	if len(proxyRuntime.feedbacks) != 1 || proxyRuntime.feedbacks[0].feedback.Kind != controlproxy.ProxyFeedbackSuccess {
		t.Fatalf("proxy feedbacks = %#v", proxyRuntime.feedbacks)
	}
}

func TestExecuteJSONEncodesRequestWhenNoPayloadBuilder(t *testing.T) {
	accountLease := AccountLease{Idx: 11, Token: "acct-token"}
	directory := &fakeAccountDirectory{lease: &accountLease}
	proxyRuntime := &fakeProxyRuntime{}
	var payload []byte

	_ = Execute(context.Background(), executorSpec(), map[string]any{"prompt": "hi"}, ExecutorOptions{
		Planner:          fixedPlanner(executorPlan()),
		AccountDirectory: directory,
		ProxyRuntime:     proxyRuntime,
		Transport: func(_ context.Context, request TransportRequest) (any, error) {
			payload = request.Payload
			return map[string]any{}, nil
		},
		Clock: sequenceClock(0, 1),
	})

	if string(payload) != `{"prompt":"hi"}` {
		t.Fatalf("json payload = %q", payload)
	}
}

func TestExecuteClassifiesUpstreamErrorAndAppliesFeedback(t *testing.T) {
	accountLease := AccountLease{Idx: 4, Token: "acct-token"}
	proxyLease := controlproxy.NewProxyLease("proxy-lease")
	directory := &fakeAccountDirectory{lease: &accountLease}
	proxyRuntime := &fakeProxyRuntime{lease: &proxyLease}

	result := Execute(context.Background(), executorSpec(), map[string]any{}, ExecutorOptions{
		Planner:          fixedPlanner(executorPlan()),
		AccountDirectory: directory,
		ProxyRuntime:     proxyRuntime,
		Transport: func(context.Context, TransportRequest) (any, error) {
			return nil, &UpstreamError{Status: 429, Details: map[string]string{"body": "quota"}, Message: "quota hit"}
		},
		Clock: sequenceClock(10, 30),
	})

	if result.Category != ResultCategoryRateLimited || result.StatusCode != 429 || result.Body != "quota" || result.Error != "quota hit" {
		t.Fatalf("upstream error result = %#v", result)
	}
	if len(directory.feedbacks) != 1 || directory.feedbacks[0].kind != AccountFeedbackRateLimited {
		t.Fatalf("account feedbacks = %#v", directory.feedbacks)
	}
	if len(proxyRuntime.feedbacks) != 1 {
		t.Fatalf("proxy feedbacks = %#v", proxyRuntime.feedbacks)
	}
	proxyFeedback := proxyRuntime.feedbacks[0].feedback
	if proxyFeedback.Kind != controlproxy.ProxyFeedbackRateLimited || proxyFeedback.StatusCode == nil || *proxyFeedback.StatusCode != 429 {
		t.Fatalf("proxy feedback = %#v", proxyFeedback)
	}
}

func TestExecuteClassifiesValueUpstreamErrorLikePythonException(t *testing.T) {
	accountLease := AccountLease{Idx: 4, Token: "acct-token"}
	proxyLease := controlproxy.NewProxyLease("proxy-lease")
	directory := &fakeAccountDirectory{lease: &accountLease}
	proxyRuntime := &fakeProxyRuntime{lease: &proxyLease}

	result := Execute(context.Background(), executorSpec(), map[string]any{}, ExecutorOptions{
		Planner:          fixedPlanner(executorPlan()),
		AccountDirectory: directory,
		ProxyRuntime:     proxyRuntime,
		Transport: func(context.Context, TransportRequest) (any, error) {
			return nil, UpstreamError{Status: 403, Details: map[string]string{"body": "token expired"}, Message: "forbidden"}
		},
		Clock: sequenceClock(10, 30),
	})

	if result.Category != ResultCategoryAuthFailure || result.StatusCode != 403 || result.Body != "token expired" || result.Error != "forbidden" {
		t.Fatalf("value upstream error result = %#v", result)
	}
	if len(directory.feedbacks) != 1 || directory.feedbacks[0].kind != AccountFeedbackUnauthorized {
		t.Fatalf("account feedbacks = %#v", directory.feedbacks)
	}
	if len(proxyRuntime.feedbacks) != 1 {
		t.Fatalf("proxy feedbacks = %#v", proxyRuntime.feedbacks)
	}
	proxyFeedback := proxyRuntime.feedbacks[0].feedback
	if proxyFeedback.Kind != controlproxy.ProxyFeedbackUnauthorized || proxyFeedback.StatusCode == nil || *proxyFeedback.StatusCode != 403 {
		t.Fatalf("proxy feedback = %#v", proxyFeedback)
	}
}

func TestExecuteFeedbackErrorsAreBestEffort(t *testing.T) {
	accountLease := AccountLease{Idx: 4, Token: "acct-token"}
	directory := &fakeAccountDirectory{lease: &accountLease, releaseErr: errors.New("release failed")}
	proxyRuntime := &fakeProxyRuntime{}

	result := Execute(context.Background(), executorSpec(), map[string]any{}, ExecutorOptions{
		Planner:          fixedPlanner(executorPlan()),
		AccountDirectory: directory,
		ProxyRuntime:     proxyRuntime,
		Transport: func(context.Context, TransportRequest) (any, error) {
			return "ok", nil
		},
		Clock: sequenceClock(20, 50),
	})

	if result.Category != ResultCategorySuccess || result.Payload != "ok" {
		t.Fatalf("result should survive feedback error: %#v", result)
	}
	if directory.releaseCount != 1 || len(directory.feedbacks) != 0 || len(proxyRuntime.feedbacks) != 0 {
		t.Fatalf("feedback should stop after release error: releases=%d account=%#v proxy=%#v", directory.releaseCount, directory.feedbacks, proxyRuntime.feedbacks)
	}
}

func assertTransportRequest(t *testing.T, request TransportRequest, payload []byte) {
	t.Helper()
	if request.Endpoint != "https://upstream.test/reverse" || request.AccountToken != "acct-token" {
		t.Fatalf("transport identity = %#v", request)
	}
	if string(request.Payload) != string(payload) {
		t.Fatalf("payload = %q, want %q", request.Payload, payload)
	}
	if request.ProxyLease == nil || request.ProxyLease.LeaseID != "proxy-lease" {
		t.Fatalf("proxy lease = %#v", request.ProxyLease)
	}
	if request.TimeoutS != 9.5 || request.ContentType != "application/custom" ||
		request.Origin != "https://origin.test" || request.Referer != "https://referer.test/" {
		t.Fatalf("transport metadata = %#v", request)
	}
}

func executorSpec() controlmodel.ModelSpec {
	return controlmodel.ModelSpec{
		ModeID:     controlmodel.ModeExpert,
		Tier:       controlmodel.TierSuper,
		Capability: controlmodel.CapabilityChat,
	}
}

func executorPlan() ReversePlan {
	plan := NewReversePlan("https://upstream.test/reverse", TransportKindHTTPJSON, []int{7, 3}, 2)
	plan.TimeoutS = 9.5
	plan.ContentType = "application/custom"
	plan.Origin = "https://origin.test"
	plan.Referer = "https://referer.test/"
	return plan
}

func fixedPlanner(plan ReversePlan) func(controlmodel.ModelSpec, map[string]any) ReversePlan {
	return func(controlmodel.ModelSpec, map[string]any) ReversePlan { return plan }
}

func sequenceClock(values ...int64) func() int64 {
	index := 0
	return func() int64 {
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

type fakeAccountDirectory struct {
	lease        *AccountLease
	reservePool  []int
	reserveMode  int
	released     AccountLease
	releaseErr   error
	releaseCount int
	feedbacks    []accountFeedbackCall
}

func (d *fakeAccountDirectory) Reserve(_ context.Context, poolCandidates []int, modeID int) (*AccountLease, error) {
	d.reservePool = append([]int(nil), poolCandidates...)
	d.reserveMode = modeID
	return d.lease, nil
}

func (d *fakeAccountDirectory) Release(_ context.Context, lease AccountLease) error {
	d.releaseCount++
	d.released = lease
	return d.releaseErr
}

func (d *fakeAccountDirectory) Feedback(_ context.Context, token string, kind AccountFeedbackKind, modeID int) error {
	d.feedbacks = append(d.feedbacks, accountFeedbackCall{token: token, kind: kind, modeID: modeID})
	return nil
}

type accountFeedbackCall struct {
	token  string
	kind   AccountFeedbackKind
	modeID int
}

type fakeProxyRuntime struct {
	lease        *controlproxy.ProxyLease
	acquireCount int
	feedbacks    []proxyFeedbackCall
}

func (r *fakeProxyRuntime) Acquire(context.Context) (*controlproxy.ProxyLease, error) {
	r.acquireCount++
	return r.lease, nil
}

func (r *fakeProxyRuntime) Feedback(_ context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedbacks = append(r.feedbacks, proxyFeedbackCall{lease: lease, feedback: feedback})
	return nil
}

type proxyFeedbackCall struct {
	lease    controlproxy.ProxyLease
	feedback controlproxy.ProxyFeedback
}
