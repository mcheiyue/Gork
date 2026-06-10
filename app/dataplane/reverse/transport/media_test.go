package transport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestCreateMediaPostUsesPayloadRefererProxyAndFeedback(t *testing.T) {
	runtime := newFakeMediaProxyRuntime("media-lease")
	client := &fakeMediaHTTPClient{result: map[string]any{"postId": "post-1"}}

	result, err := CreateMediaPost(context.Background(), "token", "image", MediaOptions{
		ProxyRuntime: runtime,
		Client:       client,
		Timeout:      11 * time.Second,
		MediaURL:     "https://cdn.test/image.png",
		Prompt:       "make it cinematic",
	})
	if err != nil {
		t.Fatalf("CreateMediaPost returned error: %v", err)
	}
	if !reflect.DeepEqual(result, map[string]any{"postId": "post-1"}) {
		t.Fatalf("media post result = %#v", result)
	}
	request := client.posts[0]
	if request.URL != protocol.MediaPostURL || request.Token != "token" || request.Timeout != 11*time.Second {
		t.Fatalf("media post request = %#v", request)
	}
	if request.Origin != "https://grok.com" || request.Referer != "https://grok.com/imagine" {
		t.Fatalf("media post origin/referer = %#v", request)
	}
	assertMediaJSONPayload(t, request.Payload, map[string]any{
		"mediaType": "image",
		"mediaUrl":  "https://cdn.test/image.png",
		"prompt":    "make it cinematic",
	})
	assertMediaAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeApp, controlproxy.RequestKindHTTP)
	assertMediaFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, mediaIntPtr(200))
}

func TestCreateMediaLinkAndUpscaleVideoUseProtocolPayloads(t *testing.T) {
	runtime := newFakeMediaProxyRuntime("link-lease", "upscale-lease")
	client := &fakeMediaHTTPClient{result: map[string]any{"ok": true}}

	if _, err := CreateMediaLink(context.Background(), "token", "post-1", MediaOptions{ProxyRuntime: runtime, Client: client}); err != nil {
		t.Fatalf("CreateMediaLink returned error: %v", err)
	}
	if _, err := UpscaleVideo(context.Background(), "token", "video-1", MediaOptions{ProxyRuntime: runtime, Client: client}); err != nil {
		t.Fatalf("UpscaleVideo returned error: %v", err)
	}

	if client.posts[0].URL != protocol.MediaLinkURL || client.posts[0].Referer != "https://grok.com" {
		t.Fatalf("link request = %#v", client.posts[0])
	}
	assertMediaJSONPayload(t, client.posts[0].Payload, map[string]any{"postId": "post-1", "source": "post-page", "platform": "web"})
	if client.posts[1].URL != protocol.VideoUpscaleURL || client.posts[1].Referer != "https://grok.com" {
		t.Fatalf("upscale request = %#v", client.posts[1])
	}
	assertMediaJSONPayload(t, client.posts[1].Payload, map[string]any{"videoId": "video-1"})
	assertMediaFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, mediaIntPtr(200))
	assertMediaFeedback(t, runtime.feedbacks, 1, controlproxy.ProxyFeedbackSuccess, mediaIntPtr(200))
}

func TestMediaPostWithProxyAppliesUpstreamAndTransportFeedback(t *testing.T) {
	upstreamRuntime := newFakeMediaProxyRuntime("upstream-lease")
	upstreamClient := &fakeMediaHTTPClient{err: platform.NewUpstreamError("forbidden", 403, "blocked")}
	_, err := CreateMediaLink(context.Background(), "token", "post-1", MediaOptions{ProxyRuntime: upstreamRuntime, Client: upstreamClient})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 403 {
		t.Fatalf("upstream error = %#v", err)
	}
	assertMediaFeedback(t, upstreamRuntime.feedbacks, 0, controlproxy.ProxyFeedbackChallenge, mediaIntPtr(403))

	transportRuntime := newFakeMediaProxyRuntime("transport-lease")
	transportClient := &fakeMediaHTTPClient{err: errors.New("dial failed")}
	_, err = UpscaleVideo(context.Background(), "token", "video-1", MediaOptions{ProxyRuntime: transportRuntime, Client: transportClient})
	if !errors.As(err, &upstream) || upstream.Status != 502 || !strings.Contains(upstream.Message, "upscale_video: transport error: dial failed") {
		t.Fatalf("transport error = %#v", err)
	}
	assertMediaFeedback(t, transportRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestMediaDefaultTimeoutUsesVideoConfigAndOmitsEmptyOptionalPayloadFields(t *testing.T) {
	resetMediaTimeoutProviderForTest(t, 12.5)
	runtime := newFakeMediaProxyRuntime("default-lease")
	client := &fakeMediaHTTPClient{result: map[string]any{"postId": "post-1"}}

	if _, err := CreateMediaPost(context.Background(), "token", "image", MediaOptions{
		ProxyRuntime: runtime,
		Client:       client,
	}); err != nil {
		t.Fatalf("CreateMediaPost returned error: %v", err)
	}

	request := client.posts[0]
	if request.Timeout != 12500*time.Millisecond {
		t.Fatalf("media timeout = %s, want configured 12.5s", request.Timeout)
	}
	assertMediaJSONPayload(t, request.Payload, map[string]any{"mediaType": "image"})
}

func TestMediaExplicitTimeoutOverridesVideoConfig(t *testing.T) {
	resetMediaTimeoutProviderForTest(t, 99)

	options := normalizeMediaOptions(MediaOptions{Timeout: 3 * time.Second})

	if options.Timeout != 3*time.Second {
		t.Fatalf("media timeout = %s, want explicit 3s", options.Timeout)
	}
}

func TestMediaInvalidConfiguredTimeoutFallsBackToPythonDefault(t *testing.T) {
	resetMediaTimeoutProviderForTest(t, -1)

	options := normalizeMediaOptions(MediaOptions{})

	if options.Timeout != defaultMediaTimeout {
		t.Fatalf("media timeout = %s, want %s", options.Timeout, defaultMediaTimeout)
	}
}

type fakeMediaProxyRuntime struct {
	leases    []controlproxy.ProxyLease
	acquires  []controlproxy.AcquireOptions
	feedbacks []controlproxy.ProxyFeedback
}

func newFakeMediaProxyRuntime(leaseIDs ...string) *fakeMediaProxyRuntime {
	runtime := &fakeMediaProxyRuntime{}
	for _, leaseID := range leaseIDs {
		runtime.leases = append(runtime.leases, controlproxy.NewProxyLease(leaseID))
	}
	return runtime
}

func (r *fakeMediaProxyRuntime) Acquire(_ context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	if len(options) > 0 {
		r.acquires = append(r.acquires, options[0])
	} else {
		r.acquires = append(r.acquires, controlproxy.AcquireOptions{})
	}
	if len(r.leases) == 0 {
		return controlproxy.ProxyLease{}, errors.New("no fake media lease")
	}
	lease := r.leases[0]
	r.leases = r.leases[1:]
	return lease, nil
}

func (r *fakeMediaProxyRuntime) Feedback(_ context.Context, _ controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedbacks = append(r.feedbacks, feedback)
	return nil
}

func resetMediaTimeoutProviderForTest(t *testing.T, timeout float64) {
	t.Helper()
	previous := mediaTimeoutProvider
	mediaTimeoutProvider = func(float64) float64 { return timeout }
	t.Cleanup(func() {
		mediaTimeoutProvider = previous
	})
}

type fakeMediaHTTPClient struct {
	posts  []MediaHTTPRequest
	result map[string]any
	err    error
}

func (c *fakeMediaHTTPClient) PostJSON(_ context.Context, request MediaHTTPRequest) (map[string]any, error) {
	c.posts = append(c.posts, request)
	if c.err != nil {
		return nil, c.err
	}
	return c.result, nil
}

func assertMediaJSONPayload(t *testing.T, payload []byte, want map[string]any) {
	t.Helper()
	got := map[string]any{}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("payload JSON decode failed: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("payload mismatch\nwant: %#v\n got: %#v", want, got)
	}
}

func assertMediaAcquire(t *testing.T, got controlproxy.AcquireOptions, scope controlproxy.ProxyScope, kind controlproxy.RequestKind) {
	t.Helper()
	if got.Scope != scope || got.Kind != kind {
		t.Fatalf("acquire options = %#v, want scope=%s kind=%s", got, scope, kind)
	}
}

func assertMediaFeedback(t *testing.T, feedbacks []controlproxy.ProxyFeedback, index int, kind controlproxy.ProxyFeedbackKind, status *int) {
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

func mediaIntPtr(value int) *int {
	return &value
}
