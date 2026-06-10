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

func TestStreamImagesSendsResetRequestAndYieldsProgressFinal(t *testing.T) {
	runtime := newFakeImagineProxyRuntime("imagine-lease")
	imageID := "123e4567-e89b-12d3-a456-426614174000"
	conn := &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"json","current_status":"start_stage","image_id":"` + imageID + `","order":0,"width":1024,"height":1536}`),
		imagineText(`{"type":"image","url":"https://assets.grok.test/images/` + imageID + `.png","blob":"blob-data","percentage_complete":"62"}`),
		imagineText(`{"type":"json","current_status":"completed","image_id":"` + imageID + `","moderated":false,"r_rated":true}`),
		{err: context.DeadlineExceeded},
	}}
	client := &fakeImagineWebSocketClient{connections: []*fakeImagineWebSocketConn{conn}}

	events, err := StreamImages(context.Background(), "token", "paint a cat", ImagineOptions{
		ProxyRuntime:  runtime,
		Client:        client,
		Timeout:       2 * time.Second,
		StreamTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("StreamImages returned error: %v", err)
	}

	if len(client.requests) != 1 || client.requests[0].URL != protocol.WSImagineURL {
		t.Fatalf("connect requests = %#v", client.requests)
	}
	request := client.requests[0]
	if !strings.Contains(request.Headers["Cookie"], "sso=token") {
		t.Fatalf("websocket headers = %#v", request.Headers)
	}
	if request.Timeout != 2*time.Second {
		t.Fatalf("connect timeout = %s, want 2s", request.Timeout)
	}
	if request.WSOptions.Heartbeat != 20*time.Second {
		t.Fatalf("websocket heartbeat = %s, want 20s", request.WSOptions.Heartbeat)
	}
	if request.WSOptions.ReceiveTimeout != 10*time.Millisecond {
		t.Fatalf("websocket receive timeout = %s, want 10ms", request.WSOptions.ReceiveTimeout)
	}
	if len(conn.sent) != 2 {
		t.Fatalf("sent messages = %#v", conn.sent)
	}
	assertImagineResetMessage(t, conn.sent[0])
	assertImaginePromptMessage(t, conn.sent[1], "paint a cat", "2:3", true, false)

	want := []map[string]any{
		{"type": "progress", "image_id": imageID, "order": 0, "progress": 10},
		{"type": "progress", "image_id": imageID, "order": 0, "progress": 62},
		{"type": "image", "image_id": imageID, "order": 0, "stage": "final", "blob": "blob-data", "url": "https://assets.grok.test/images/" + imageID + ".png", "width": 1024, "height": 1536, "is_final": true, "moderated": false, "r_rated": true},
	}
	if !reflect.DeepEqual(want, events) {
		t.Fatalf("events mismatch\nwant: %#v\n got: %#v", want, events)
	}
	assertImagineAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeApp, controlproxy.RequestKindWebSocket)
	assertImagineFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, imagineIntPtr(200))
}

func TestStreamImagesConnectFailureYieldsErrorAndFeedback(t *testing.T) {
	runtime := newFakeImagineProxyRuntime("rate-lease")
	client := &fakeImagineWebSocketClient{connectErr: platform.NewUpstreamError("connect failed", 429, "limited")}

	events, err := StreamImages(context.Background(), "token", "prompt", ImagineOptions{
		ProxyRuntime: runtime,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("StreamImages returned error: %v", err)
	}

	want := []map[string]any{{"type": "error", "error_code": "rate_limit_exceeded", "error": "connect failed"}}
	if !reflect.DeepEqual(want, events) {
		t.Fatalf("connect error events mismatch\nwant: %#v\n got: %#v", want, events)
	}
	assertImagineFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackRateLimited, imagineIntPtr(429))
}

func TestStreamImagesReconnectsWhenServerClosesBeforeEnoughImages(t *testing.T) {
	runtime := newFakeImagineProxyRuntime("first-lease", "second-lease")
	firstID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	secondID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	first := imagineCompletedConn(firstID, true)
	second := imagineCompletedConn(secondID, false)
	client := &fakeImagineWebSocketClient{connections: []*fakeImagineWebSocketConn{first, second}}

	events, err := StreamImages(context.Background(), "token", "prompt", ImagineOptions{
		ProxyRuntime:  runtime,
		Client:        client,
		Timeout:       2 * time.Second,
		StreamTimeout: 10 * time.Millisecond,
		N:             2,
	})
	if err != nil {
		t.Fatalf("StreamImages returned error: %v", err)
	}

	finals := 0
	for _, event := range events {
		if event["is_final"] == true {
			finals++
		}
	}
	if finals != 2 {
		t.Fatalf("final image count = %d, events = %#v", finals, events)
	}
	if len(runtime.acquires) != 2 || len(client.requests) != 2 {
		t.Fatalf("reconnect did not reacquire/reconnect: acquires=%#v requests=%#v", runtime.acquires, client.requests)
	}
	assertImagineFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, imagineIntPtr(200))
	assertImagineFeedback(t, runtime.feedbacks, 1, controlproxy.ProxyFeedbackSuccess, imagineIntPtr(200))
}

func TestStreamImagineRoundHandlesSendFailureAndSlotTimeout(t *testing.T) {
	sendFailure := &fakeImagineWebSocketConn{sendErr: errors.New("write failed")}
	result, err := streamImagineRound(context.Background(), sendFailure, imagineRoundOptions{
		Prompt:         "prompt",
		AspectRatio:    "2:3",
		EnableNSFW:     true,
		Needed:         1,
		StreamTimeout:  time.Millisecond,
		RoundTimeout:   time.Second,
		InterRoundWait: time.Millisecond,
		Now:            time.Now,
	})
	if err != nil {
		t.Fatalf("streamImagineRound send failure returned error: %v", err)
	}
	if !result.WSClosed || !reflect.DeepEqual(result.Events, []map[string]any{{"type": "error", "error_code": "send_failed", "error": "write failed"}}) {
		t.Fatalf("send failure result = %#v", result)
	}

	imageID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	clock := &fakeImagineClock{values: []time.Time{
		time.Unix(100, 0),
		time.Unix(100, 0),
		time.Unix(103, 0),
	}}
	timeoutConn := &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"json","current_status":"start_stage","image_id":"` + imageID + `","order":1,"width":512,"height":768}`),
	}}
	result, err = streamImagineRound(context.Background(), timeoutConn, imagineRoundOptions{
		Prompt:         "prompt",
		AspectRatio:    "2:3",
		EnableNSFW:     true,
		Needed:         1,
		StreamTimeout:  time.Millisecond,
		RoundTimeout:   time.Second,
		InterRoundWait: time.Millisecond,
		Now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("streamImagineRound timeout returned error: %v", err)
	}
	want := []map[string]any{
		{"type": "progress", "image_id": imageID, "order": 1, "progress": 10},
		{"type": "error", "error_code": "slot_incomplete", "error": "slot cccccccc timed out"},
	}
	if result.WSClosed || !reflect.DeepEqual(want, result.Events) {
		t.Fatalf("timeout result mismatch\nwant events: %#v\n got result: %#v", want, result)
	}
}

func TestStreamImagineRoundHandlesModeratedAndServerErrorFrames(t *testing.T) {
	moderatedID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	moderatedConn := &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"json","current_status":"start_stage","image_id":"` + moderatedID + `","order":2,"width":640,"height":480}`),
		imagineText(`{"type":"json","current_status":"completed","image_id":"` + moderatedID + `","moderated":true}`),
		{err: context.DeadlineExceeded},
	}}
	result, err := streamImagineRound(context.Background(), moderatedConn, imagineRoundOptions{
		Prompt:         "prompt",
		StreamTimeout:  time.Millisecond,
		RoundTimeout:   time.Second,
		InterRoundWait: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("streamImagineRound moderated returned error: %v", err)
	}
	wantModerated := []map[string]any{
		{"type": "progress", "image_id": moderatedID, "order": 2, "progress": 10},
		{"type": "moderated", "image_id": moderatedID, "order": 2},
	}
	if result.WSClosed || !reflect.DeepEqual(wantModerated, result.Events) {
		t.Fatalf("moderated result mismatch\nwant events: %#v\n got result: %#v", wantModerated, result)
	}

	errorConn := &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"error","err_code":"server_error","err_msg":"boom"}`),
	}}
	result, err = streamImagineRound(context.Background(), errorConn, imagineRoundOptions{
		Prompt:        "prompt",
		StreamTimeout: time.Millisecond,
		RoundTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("streamImagineRound server error returned error: %v", err)
	}
	wantError := []map[string]any{{"type": "error", "error_code": "server_error", "error": "boom"}}
	if !result.WSClosed || !reflect.DeepEqual(wantError, result.Events) {
		t.Fatalf("server error result mismatch\nwant events: %#v\n got result: %#v", wantError, result)
	}
}

func TestStreamImagineRoundEmitsBestEffortFinalsOnTimeoutAndWebSocketClose(t *testing.T) {
	timeoutID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	clock := &fakeImagineClock{values: []time.Time{
		time.Unix(200, 0),
		time.Unix(200, 0),
		time.Unix(200, 0),
		time.Unix(202, 0),
	}}
	timeoutConn := &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"json","current_status":"start_stage","image_id":"` + timeoutID + `","order":3,"width":320,"height":240}`),
		imagineText(`{"type":"image","url":"https://assets.grok.test/images/` + timeoutID + `.png","blob":"timeout-blob","percentage_complete":81}`),
	}}
	result, err := streamImagineRound(context.Background(), timeoutConn, imagineRoundOptions{
		Prompt:         "prompt",
		StreamTimeout:  time.Millisecond,
		RoundTimeout:   time.Second,
		InterRoundWait: time.Millisecond,
		Now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("streamImagineRound timeout best-effort returned error: %v", err)
	}
	wantTimeout := []map[string]any{
		{"type": "progress", "image_id": timeoutID, "order": 3, "progress": 10},
		{"type": "progress", "image_id": timeoutID, "order": 3, "progress": 81},
		{"type": "image", "image_id": timeoutID, "order": 3, "stage": "final", "blob": "timeout-blob", "url": "https://assets.grok.test/images/" + timeoutID + ".png", "width": 320, "height": 240, "is_final": true, "moderated": false, "r_rated": false},
	}
	if result.WSClosed || !reflect.DeepEqual(wantTimeout, result.Events) {
		t.Fatalf("timeout best-effort result mismatch\nwant events: %#v\n got result: %#v", wantTimeout, result)
	}

	closedID := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	closedConn := &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"json","current_status":"start_stage","image_id":"` + closedID + `","order":4,"width":1024,"height":1024}`),
		imagineText(`{"type":"image","url":"https://assets.grok.test/images/` + closedID + `.jpg","blob":"closed-blob","percentage_complete":77}`),
		{message: ImagineWebSocketMessage{Type: ImagineWebSocketClosedMessage}},
	}}
	result, err = streamImagineRound(context.Background(), closedConn, imagineRoundOptions{
		Prompt:        "prompt",
		StreamTimeout: time.Millisecond,
		RoundTimeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("streamImagineRound closed best-effort returned error: %v", err)
	}
	wantClosed := []map[string]any{
		{"type": "progress", "image_id": closedID, "order": 4, "progress": 10},
		{"type": "progress", "image_id": closedID, "order": 4, "progress": 77},
		{"type": "image", "image_id": closedID, "order": 4, "stage": "final", "blob": "closed-blob", "url": "https://assets.grok.test/images/" + closedID + ".jpg", "width": 1024, "height": 1024, "is_final": true, "moderated": false, "r_rated": false},
	}
	if !result.WSClosed || !reflect.DeepEqual(wantClosed, result.Events) {
		t.Fatalf("closed best-effort result mismatch\nwant events: %#v\n got result: %#v", wantClosed, result)
	}
}

func TestNormalizeImagineOptionsUsesConfiguredTimeoutDefaults(t *testing.T) {
	resetImagineTimeoutsForTest(t, 7.5, 0.25)

	options := normalizeImagineOptions(ImagineOptions{})

	if options.Timeout != 7500*time.Millisecond {
		t.Fatalf("Timeout = %s, want 7.5s", options.Timeout)
	}
	if options.StreamTimeout != 250*time.Millisecond {
		t.Fatalf("StreamTimeout = %s, want 250ms", options.StreamTimeout)
	}
}

func TestNormalizeImagineOptionsKeepsExplicitTimeouts(t *testing.T) {
	resetImagineTimeoutsForTest(t, 99, 88)

	options := normalizeImagineOptions(ImagineOptions{
		Timeout:       3 * time.Second,
		StreamTimeout: 4 * time.Second,
	})

	if options.Timeout != 3*time.Second {
		t.Fatalf("Timeout = %s, want explicit 3s", options.Timeout)
	}
	if options.StreamTimeout != 4*time.Second {
		t.Fatalf("StreamTimeout = %s, want explicit 4s", options.StreamTimeout)
	}
}

func TestNormalizeImagineOptionsFallsBackWhenConfiguredTimeoutsInvalid(t *testing.T) {
	resetImagineTimeoutsForTest(t, 0, -1)

	options := normalizeImagineOptions(ImagineOptions{})

	if options.Timeout != defaultImagineTimeout {
		t.Fatalf("Timeout = %s, want %s", options.Timeout, defaultImagineTimeout)
	}
	if options.StreamTimeout != defaultImagineStreamTimeout {
		t.Fatalf("StreamTimeout = %s, want %s", options.StreamTimeout, defaultImagineStreamTimeout)
	}
}

func TestImagineProgressClampsPythonRange(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  int
	}{
		{name: "missing defaults to midpoint", value: nil, want: 50},
		{name: "low string clamps to ten", value: "5", want: 10},
		{name: "high float clamps to ninety nine", value: 120.0, want: 99},
		{name: "integer stays in range", value: 42, want: 42},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := imagineProgress(tt.value); got != tt.want {
				t.Fatalf("imagineProgress(%#v) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

type fakeImagineWebSocketClient struct {
	requests    []ImagineWebSocketConnectRequest
	connections []*fakeImagineWebSocketConn
	connectErr  error
}

func (c *fakeImagineWebSocketClient) Connect(_ context.Context, request ImagineWebSocketConnectRequest) (ImagineWebSocketConnection, error) {
	c.requests = append(c.requests, request)
	if c.connectErr != nil {
		return nil, c.connectErr
	}
	if len(c.connections) == 0 {
		return nil, errors.New("no fake websocket connection")
	}
	conn := c.connections[0]
	c.connections = c.connections[1:]
	return conn, nil
}

type fakeImagineReceive struct {
	message ImagineWebSocketMessage
	err     error
}

type fakeImagineWebSocketConn struct {
	sent     []map[string]any
	messages []fakeImagineReceive
	sendErr  error
	closed   bool
}

func (c *fakeImagineWebSocketConn) SendJSON(_ context.Context, payload map[string]any) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, payload)
	return nil
}

func (c *fakeImagineWebSocketConn) Receive(_ context.Context, _ time.Duration) (ImagineWebSocketMessage, error) {
	if len(c.messages) == 0 {
		return ImagineWebSocketMessage{}, context.DeadlineExceeded
	}
	next := c.messages[0]
	c.messages = c.messages[1:]
	return next.message, next.err
}

func (c *fakeImagineWebSocketConn) Close() error {
	c.closed = true
	return nil
}

type fakeImagineProxyRuntime struct {
	leases    []controlproxy.ProxyLease
	acquires  []controlproxy.AcquireOptions
	feedbacks []controlproxy.ProxyFeedback
}

func newFakeImagineProxyRuntime(leaseIDs ...string) *fakeImagineProxyRuntime {
	runtime := &fakeImagineProxyRuntime{}
	for _, leaseID := range leaseIDs {
		runtime.leases = append(runtime.leases, controlproxy.NewProxyLease(leaseID))
	}
	return runtime
}

func (r *fakeImagineProxyRuntime) Acquire(_ context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	if len(options) > 0 {
		r.acquires = append(r.acquires, options[0])
	} else {
		r.acquires = append(r.acquires, controlproxy.AcquireOptions{})
	}
	if len(r.leases) == 0 {
		return controlproxy.ProxyLease{}, errors.New("no fake lease")
	}
	lease := r.leases[0]
	r.leases = r.leases[1:]
	return lease, nil
}

func (r *fakeImagineProxyRuntime) Feedback(_ context.Context, _ controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedbacks = append(r.feedbacks, feedback)
	return nil
}

type fakeImagineClock struct {
	values []time.Time
}

func (c *fakeImagineClock) Now() time.Time {
	if len(c.values) == 0 {
		return time.Unix(999, 0)
	}
	value := c.values[0]
	c.values = c.values[1:]
	return value
}

func imagineText(data string) fakeImagineReceive {
	return fakeImagineReceive{message: ImagineWebSocketMessage{Type: ImagineWebSocketTextMessage, Data: data}}
}

func resetImagineTimeoutsForTest(t *testing.T, timeout, streamTimeout float64) {
	t.Helper()
	previousTimeoutProvider := imagineTimeoutProvider
	previousStreamTimeoutProvider := imagineStreamTimeoutProvider
	imagineTimeoutProvider = func() float64 { return timeout }
	imagineStreamTimeoutProvider = func() float64 { return streamTimeout }
	t.Cleanup(func() {
		imagineTimeoutProvider = previousTimeoutProvider
		imagineStreamTimeoutProvider = previousStreamTimeoutProvider
	})
}

func imagineCompletedConn(imageID string, closes bool) *fakeImagineWebSocketConn {
	probe := fakeImagineReceive{err: context.DeadlineExceeded}
	if closes {
		probe = fakeImagineReceive{message: ImagineWebSocketMessage{Type: ImagineWebSocketClosedMessage}}
	}
	return &fakeImagineWebSocketConn{messages: []fakeImagineReceive{
		imagineText(`{"type":"json","current_status":"start_stage","image_id":"` + imageID + `","order":0,"width":1024,"height":1536}`),
		imagineText(`{"type":"image","url":"https://assets.grok.test/images/` + imageID + `.jpg","blob":"blob-` + imageID[:4] + `","percentage_complete":99}`),
		imagineText(`{"type":"json","current_status":"completed","image_id":"` + imageID + `","moderated":false,"r_rated":false}`),
		probe,
	}}
}

func assertImagineResetMessage(t *testing.T, payload map[string]any) {
	t.Helper()
	if payload["type"] != "conversation.item.create" {
		t.Fatalf("reset payload type = %#v", payload)
	}
	item, ok := payload["item"].(map[string]any)
	if !ok || item["type"] != "message" {
		t.Fatalf("reset item = %#v", payload["item"])
	}
	content := item["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "reset" {
		t.Fatalf("reset content = %#v", content)
	}
}

func assertImaginePromptMessage(t *testing.T, payload map[string]any, prompt, aspectRatio string, enableNSFW, enablePro bool) {
	t.Helper()
	item := payload["item"].(map[string]any)
	content := item["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("prompt content = %#v", content)
	}
	block := content[0]
	properties := block["properties"].(map[string]any)
	if block["text"] != prompt || block["type"] != "input_text" || properties["aspect_ratio"] != aspectRatio ||
		properties["enable_nsfw"] != enableNSFW || properties["enable_pro"] != enablePro {
		t.Fatalf("prompt message = %#v", payload)
	}
}

func assertImagineAcquire(t *testing.T, got controlproxy.AcquireOptions, scope controlproxy.ProxyScope, kind controlproxy.RequestKind) {
	t.Helper()
	if got.Scope != scope || got.Kind != kind {
		t.Fatalf("acquire options = %#v, want scope=%s kind=%s", got, scope, kind)
	}
}

func assertImagineFeedback(t *testing.T, feedbacks []controlproxy.ProxyFeedback, index int, kind controlproxy.ProxyFeedbackKind, status *int) {
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

func imagineIntPtr(value int) *int {
	return &value
}
