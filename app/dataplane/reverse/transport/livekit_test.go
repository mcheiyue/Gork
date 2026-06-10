package transport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestFetchLiveKitTokenPostsPayloadAndReportsSuccess(t *testing.T) {
	runtime := newFakeLiveKitProxyRuntime("token-lease")
	client := &fakeLiveKitHTTPClient{result: map[string]any{"access_token": "lk-token"}}

	result, err := FetchLiveKitToken(context.Background(), "sso-token", LiveKitOptions{
		ProxyRuntime:      runtime,
		Client:            client,
		Timeout:           7 * time.Second,
		Voice:             "juniper",
		Personality:       "coach",
		Speed:             1.25,
		CustomInstruction: "be brief",
	})
	if err != nil {
		t.Fatalf("FetchLiveKitToken returned error: %v", err)
	}
	if !reflect.DeepEqual(result, map[string]any{"access_token": "lk-token"}) {
		t.Fatalf("token result = %#v", result)
	}

	if len(client.posts) != 1 {
		t.Fatalf("post calls = %#v", client.posts)
	}
	request := client.posts[0]
	if request.URL != protocol.LiveKitTokenURL || request.Token != "sso-token" || request.Timeout != 7*time.Second {
		t.Fatalf("token request = %#v", request)
	}
	if request.Origin != "https://grok.com" || request.Referer != "https://grok.com/" {
		t.Fatalf("origin/referer = %#v", request)
	}
	wantPayload := protocol.BuildLiveKitTokenRequestPayload(protocol.LiveKitTokenOptions{
		Voice:             "juniper",
		Personality:       "coach",
		Speed:             1.25,
		CustomInstruction: "be brief",
	})
	if string(request.Payload) != string(wantPayload) {
		t.Fatalf("payload mismatch\nwant: %s\n got: %s", wantPayload, request.Payload)
	}
	assertLiveKitAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeApp, controlproxy.RequestKindHTTP)
	assertLiveKitFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, liveKitIntPtr(200))
}

func TestFetchLiveKitTokenAppliesUpstreamAndTransportFeedback(t *testing.T) {
	upstreamRuntime := newFakeLiveKitProxyRuntime("upstream-lease")
	upstreamClient := &fakeLiveKitHTTPClient{err: platform.NewUpstreamError("upstream returned 503", 503, "busy")}
	_, err := FetchLiveKitToken(context.Background(), "token", LiveKitOptions{ProxyRuntime: upstreamRuntime, Client: upstreamClient})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 503 {
		t.Fatalf("upstream error = %#v", err)
	}
	assertLiveKitFeedback(t, upstreamRuntime.feedbacks, 0, controlproxy.ProxyFeedbackUpstream5xx, liveKitIntPtr(503))

	transportRuntime := newFakeLiveKitProxyRuntime("transport-lease")
	transportClient := &fakeLiveKitHTTPClient{err: errors.New("dial failed")}
	_, err = FetchLiveKitToken(context.Background(), "token", LiveKitOptions{ProxyRuntime: transportRuntime, Client: transportClient})
	if !errors.As(err, &upstream) || upstream.Status != 502 || !strings.Contains(upstream.Message, "fetch_livekit_token: transport error: dial failed") {
		t.Fatalf("transport error = %#v", err)
	}
	assertLiveKitFeedback(t, transportRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestConnectLiveKitWSBuildsURLHeadersAndFeedbackOnClose(t *testing.T) {
	runtime := newFakeLiveKitProxyRuntime("ws-lease")
	client := &fakeLiveKitWebSocketClient{connection: &fakeLiveKitWebSocketConnection{}}

	conn, err := ConnectLiveKitWS(context.Background(), "sso-token", "access token", LiveKitWSOptions{
		ProxyRuntime: runtime,
		Client:       client,
		Timeout:      9 * time.Second,
	})
	if err != nil {
		t.Fatalf("ConnectLiveKitWS returned error: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("ws requests = %#v", client.requests)
	}
	request := client.requests[0]
	if request.URL != protocol.BuildLiveKitWSURL("access token") || request.Timeout != 9*time.Second {
		t.Fatalf("ws request = %#v", request)
	}
	if !strings.Contains(request.Headers["Cookie"], "sso=sso-token") {
		t.Fatalf("ws headers = %#v", request.Headers)
	}
	assertLiveKitAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeApp, controlproxy.RequestKindWebSocket)
	if len(runtime.feedbacks) != 0 {
		t.Fatalf("feedback before close = %#v", runtime.feedbacks)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close returned error: %v", err)
	}
	assertLiveKitFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, liveKitIntPtr(200))
}

func TestConnectLiveKitWSCloseIgnoresFeedbackErrorLikePython(t *testing.T) {
	runtime := newFakeLiveKitProxyRuntime("ws-close-lease")
	runtime.feedbackErr = errors.New("feedback failed")
	client := &fakeLiveKitWebSocketClient{connection: &fakeLiveKitWebSocketConnection{}}

	conn, err := ConnectLiveKitWS(context.Background(), "token", "access", LiveKitWSOptions{
		ProxyRuntime: runtime,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("ConnectLiveKitWS returned error: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close returned feedback error, want nil: %v", err)
	}
	assertLiveKitFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, liveKitIntPtr(200))
}

func TestConnectLiveKitWSFailureReportsTransportFeedback(t *testing.T) {
	runtime := newFakeLiveKitProxyRuntime("ws-fail-lease")
	client := &fakeLiveKitWebSocketClient{err: errors.New("connect failed")}

	_, err := ConnectLiveKitWS(context.Background(), "token", "access", LiveKitWSOptions{
		ProxyRuntime: runtime,
		Client:       client,
	})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 502 || !strings.Contains(upstream.Message, "connect_livekit_ws: connect failed") {
		t.Fatalf("connect error = %#v", err)
	}
	assertLiveKitFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestLiveKitDefaultTimeoutsUseVoiceConfig(t *testing.T) {
	resetLiveKitTimeoutProviderForTest(t, 12.5)

	tokenRuntime := newFakeLiveKitProxyRuntime("token-default-lease")
	tokenClient := &fakeLiveKitHTTPClient{result: map[string]any{"access_token": "lk-token"}}
	_, err := FetchLiveKitToken(context.Background(), "token", LiveKitOptions{
		ProxyRuntime: tokenRuntime,
		Client:       tokenClient,
	})
	if err != nil {
		t.Fatalf("FetchLiveKitToken returned error: %v", err)
	}
	if got := tokenClient.posts[0].Timeout; got != 12500*time.Millisecond {
		t.Fatalf("token timeout = %s, want configured 12.5s", got)
	}
	wantPayload := protocol.BuildLiveKitTokenRequestPayload(protocol.LiveKitTokenOptions{})
	if string(tokenClient.posts[0].Payload) != string(wantPayload) {
		t.Fatalf("default payload mismatch\nwant: %s\n got: %s", wantPayload, tokenClient.posts[0].Payload)
	}

	wsRuntime := newFakeLiveKitProxyRuntime("ws-default-lease")
	wsClient := &fakeLiveKitWebSocketClient{connection: &fakeLiveKitWebSocketConnection{}}
	_, err = ConnectLiveKitWS(context.Background(), "token", "access", LiveKitWSOptions{
		ProxyRuntime: wsRuntime,
		Client:       wsClient,
	})
	if err != nil {
		t.Fatalf("ConnectLiveKitWS returned error: %v", err)
	}
	if got := wsClient.requests[0].Timeout; got != 12500*time.Millisecond {
		t.Fatalf("ws timeout = %s, want configured 12.5s", got)
	}
}

func TestLiveKitExplicitTimeoutsOverrideVoiceConfig(t *testing.T) {
	resetLiveKitTimeoutProviderForTest(t, 99)

	tokenOptions := normalizeLiveKitOptions(LiveKitOptions{Timeout: 3 * time.Second})
	if tokenOptions.Timeout != 3*time.Second {
		t.Fatalf("token timeout = %s, want explicit 3s", tokenOptions.Timeout)
	}

	wsOptions := normalizeLiveKitWSOptions(LiveKitWSOptions{Timeout: 4 * time.Second})
	if wsOptions.Timeout != 4*time.Second {
		t.Fatalf("ws timeout = %s, want explicit 4s", wsOptions.Timeout)
	}
}

func TestLiveKitInvalidConfiguredTimeoutFallsBackToPythonDefaults(t *testing.T) {
	resetLiveKitTimeoutProviderForTest(t, -1)

	tokenOptions := normalizeLiveKitOptions(LiveKitOptions{})
	if tokenOptions.Timeout != defaultLiveKitTokenTimeout {
		t.Fatalf("token timeout = %s, want %s", tokenOptions.Timeout, defaultLiveKitTokenTimeout)
	}

	wsOptions := normalizeLiveKitWSOptions(LiveKitWSOptions{})
	if wsOptions.Timeout != defaultLiveKitWSTimeout {
		t.Fatalf("ws timeout = %s, want %s", wsOptions.Timeout, defaultLiveKitWSTimeout)
	}
}

type fakeLiveKitProxyRuntime struct {
	leases      []controlproxy.ProxyLease
	acquires    []controlproxy.AcquireOptions
	feedbacks   []controlproxy.ProxyFeedback
	feedbackErr error
}

func newFakeLiveKitProxyRuntime(leaseIDs ...string) *fakeLiveKitProxyRuntime {
	runtime := &fakeLiveKitProxyRuntime{}
	for _, leaseID := range leaseIDs {
		runtime.leases = append(runtime.leases, controlproxy.NewProxyLease(leaseID))
	}
	return runtime
}

func (r *fakeLiveKitProxyRuntime) Acquire(_ context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	if len(options) > 0 {
		r.acquires = append(r.acquires, options[0])
	} else {
		r.acquires = append(r.acquires, controlproxy.AcquireOptions{})
	}
	if len(r.leases) == 0 {
		return controlproxy.ProxyLease{}, errors.New("no fake livekit lease")
	}
	lease := r.leases[0]
	r.leases = r.leases[1:]
	return lease, nil
}

func (r *fakeLiveKitProxyRuntime) Feedback(_ context.Context, _ controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedbacks = append(r.feedbacks, feedback)
	return r.feedbackErr
}

type fakeLiveKitHTTPClient struct {
	posts  []LiveKitHTTPRequest
	result map[string]any
	err    error
}

func (c *fakeLiveKitHTTPClient) PostJSON(_ context.Context, request LiveKitHTTPRequest) (map[string]any, error) {
	c.posts = append(c.posts, request)
	if c.err != nil {
		return nil, c.err
	}
	return c.result, nil
}

type fakeLiveKitWebSocketClient struct {
	requests   []LiveKitWebSocketRequest
	connection *fakeLiveKitWebSocketConnection
	err        error
}

func (c *fakeLiveKitWebSocketClient) Connect(_ context.Context, request LiveKitWebSocketRequest) (LiveKitWebSocketConnection, error) {
	c.requests = append(c.requests, request)
	if c.err != nil {
		return nil, c.err
	}
	if c.connection == nil {
		c.connection = &fakeLiveKitWebSocketConnection{}
	}
	c.connection.onClose = request.OnClose
	return c.connection, nil
}

type fakeLiveKitWebSocketConnection struct {
	onClose func(context.Context) error
	closed  bool
}

func (c *fakeLiveKitWebSocketConnection) Close() error {
	c.closed = true
	if c.onClose != nil {
		return c.onClose(context.Background())
	}
	return nil
}

func resetLiveKitTimeoutProviderForTest(t *testing.T, timeout float64) {
	t.Helper()
	previous := liveKitTimeoutProvider
	liveKitTimeoutProvider = func(float64) float64 { return timeout }
	t.Cleanup(func() {
		liveKitTimeoutProvider = previous
	})
}

func assertLiveKitAcquire(t *testing.T, got controlproxy.AcquireOptions, scope controlproxy.ProxyScope, kind controlproxy.RequestKind) {
	t.Helper()
	if got.Scope != scope || got.Kind != kind {
		t.Fatalf("acquire options = %#v, want scope=%s kind=%s", got, scope, kind)
	}
}

func assertLiveKitFeedback(t *testing.T, feedbacks []controlproxy.ProxyFeedback, index int, kind controlproxy.ProxyFeedbackKind, status *int) {
	t.Helper()
	if len(feedbacks) <= index {
		t.Fatalf("feedbacks = %#v, missing index %d", feedbacks, index)
	}
	feedback := feedbacks[index]
	if feedback.Kind != kind {
		t.Fatalf("feedback[%d].Kind = %s, want %s", index, feedback.Kind, kind)
	}
	if status == nil {
		if feedback.StatusCode != nil {
			t.Fatalf("feedback[%d].StatusCode = %#v, want nil", index, feedback.StatusCode)
		}
		return
	}
	if feedback.StatusCode == nil || *feedback.StatusCode != *status {
		t.Fatalf("feedback[%d].StatusCode = %#v, want %d", index, feedback.StatusCode, *status)
	}
}

func liveKitIntPtr(value int) *int {
	return &value
}
