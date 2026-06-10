package transport

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestAssetsSlotChannelsUseConfiguredConcurrencyLikePython(t *testing.T) {
	resetAssetsConcurrencyForTest(t, 2, 1)

	if cap(assetListSlotChannel()) != 2 {
		t.Fatalf("list slot cap = %d, want 2", cap(assetListSlotChannel()))
	}
	if cap(assetDeleteSlotChannel()) != 1 {
		t.Fatalf("delete slot cap = %d, want 1", cap(assetDeleteSlotChannel()))
	}

	release1, err := acquireAssetsSlot(context.Background(), assetListSlotChannel())
	if err != nil {
		t.Fatalf("first list acquire returned error: %v", err)
	}
	defer release1()
	release2, err := acquireAssetsSlot(context.Background(), assetListSlotChannel())
	if err != nil {
		t.Fatalf("second list acquire returned error: %v", err)
	}
	defer release2()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if release3, err := acquireAssetsSlot(ctx, assetListSlotChannel()); err == nil {
		release3()
		t.Fatal("third list acquire succeeded with configured concurrency 2")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third list acquire error = %v, want deadline exceeded", err)
	}
}

func TestAssetsConcurrencyFloorsInvalidConfigLikePython(t *testing.T) {
	resetAssetsConcurrencyForTest(t, 0, -5)

	if cap(assetListSlotChannel()) != 1 {
		t.Fatalf("list slot cap = %d, want 1", cap(assetListSlotChannel()))
	}
	if cap(assetDeleteSlotChannel()) != 1 {
		t.Fatalf("delete slot cap = %d, want 1", cap(assetDeleteSlotChannel()))
	}
}

func TestAssetsOptionsUseConfiguredTimeoutsLikePython(t *testing.T) {
	resetAssetsTimeoutsForTest(t, 1.5, 2.5, 3.25)

	option := assetsOptions()
	if option.ListTimeout != 1500*time.Millisecond ||
		option.DeleteTimeout != 2500*time.Millisecond ||
		option.DownloadTimeout != 3250*time.Millisecond {
		t.Fatalf("configured timeouts = %s/%s/%s", option.ListTimeout, option.DeleteTimeout, option.DownloadTimeout)
	}

	option = assetsOptions(AssetsOptions{ListTimeout: 7 * time.Second})
	if option.ListTimeout != 7*time.Second ||
		option.DeleteTimeout != 2500*time.Millisecond ||
		option.DownloadTimeout != 3250*time.Millisecond {
		t.Fatalf("partial explicit timeouts = %s/%s/%s", option.ListTimeout, option.DeleteTimeout, option.DownloadTimeout)
	}
}

func TestListAssetsUsesAssetProxyAndReportsSuccess(t *testing.T) {
	runtime := newFakeAssetsProxyRuntime("list-lease")
	client := &fakeAssetsHTTPClient{jsonResult: map[string]any{"items": []any{"a"}, "cursor": "next"}}

	result, err := ListAssets(context.Background(), "token", map[string]any{"limit": 50}, AssetsOptions{
		ProxyRuntime: runtime,
		Client:       client,
		ListTimeout:  7 * time.Second,
	})
	if err != nil {
		t.Fatalf("ListAssets returned error: %v", err)
	}
	if !reflect.DeepEqual(result, map[string]any{"items": []any{"a"}, "cursor": "next"}) {
		t.Fatalf("list result = %#v", result)
	}
	if len(client.getJSON) != 1 {
		t.Fatalf("getJSON calls = %#v", client.getJSON)
	}
	request := client.getJSON[0]
	if request.URL != protocol.AssetsListURL || request.Token != "token" || request.Timeout != 7*time.Second {
		t.Fatalf("list request = %#v", request)
	}
	if !reflect.DeepEqual(request.Params, map[string]any{"limit": 50}) {
		t.Fatalf("params = %#v", request.Params)
	}
	assertAssetsAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeAsset, controlproxy.RequestKindHTTP)
	assertAssetsFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, intPtr(200))
}

func TestDeleteAssetUsesDeleteURLAndReportsSuccess(t *testing.T) {
	runtime := newFakeAssetsProxyRuntime("delete-lease")
	client := &fakeAssetsHTTPClient{jsonResult: map[string]any{"deleted": true}}

	result, err := DeleteAsset(context.Background(), "token", "asset-1", AssetsOptions{
		ProxyRuntime:  runtime,
		Client:        client,
		DeleteTimeout: 8 * time.Second,
	})
	if err != nil {
		t.Fatalf("DeleteAsset returned error: %v", err)
	}
	if result["deleted"] != true {
		t.Fatalf("delete result = %#v", result)
	}
	request := client.deleteJSON[0]
	if request.URL != protocol.AssetDeleteURL("asset-1") || request.Timeout != 8*time.Second {
		t.Fatalf("delete request = %#v", request)
	}
	assertAssetsAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeAsset, controlproxy.RequestKindHTTP)
	assertAssetsFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, intPtr(200))
}

func TestDownloadAssetBuildsHeadersAndReturnsStream(t *testing.T) {
	runtime := newFakeAssetsProxyRuntime("download-lease")
	client := &fakeAssetsHTTPClient{stream: io.NopCloser(strings.NewReader("image-bytes"))}

	result, err := DownloadAsset(context.Background(), "token", "users/u/file.png", AssetsOptions{
		ProxyRuntime:    runtime,
		Client:          client,
		DownloadTimeout: 9 * time.Second,
	})
	if err != nil {
		t.Fatalf("DownloadAsset returned error: %v", err)
	}
	body, err := io.ReadAll(result.Stream)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(body) != "image-bytes" {
		t.Fatalf("stream body = %q", body)
	}
	if result.ContentType == nil || *result.ContentType != "image/png" {
		t.Fatalf("content type = %#v", result.ContentType)
	}
	request := client.streams[0]
	if request.URL != "https://assets.grok.com/users/u/file.png" ||
		request.Origin != "https://assets.grok.com" ||
		request.Referer != "https://assets.grok.com/" ||
		request.Timeout != 9*time.Second {
		t.Fatalf("download request = %#v", request)
	}
	if request.ExtraHeaders["Accept"] != "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8" ||
		request.ExtraHeaders["Sec-Fetch-Mode"] != "navigate" {
		t.Fatalf("download headers = %#v", request.ExtraHeaders)
	}
	assertAssetsAcquire(t, runtime.acquires[0], controlproxy.ProxyScopeAsset, controlproxy.RequestKindHTTP)
	assertAssetsFeedback(t, runtime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, intPtr(200))
}

func TestListAssetsAppliesUpstreamAndTransportFeedback(t *testing.T) {
	upstreamRuntime := newFakeAssetsProxyRuntime("upstream-lease")
	upstreamClient := &fakeAssetsHTTPClient{err: platform.NewUpstreamError("upstream failed", 503, "body")}
	_, err := ListAssets(context.Background(), "token", nil, AssetsOptions{ProxyRuntime: upstreamRuntime, Client: upstreamClient})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 503 {
		t.Fatalf("upstream error = %T %v", err, err)
	}
	assertAssetsFeedback(t, upstreamRuntime.feedbacks, 0, controlproxy.ProxyFeedbackUpstream5xx, intPtr(503))

	transportRuntime := newFakeAssetsProxyRuntime("transport-lease")
	transportClient := &fakeAssetsHTTPClient{err: errors.New("dial failed")}
	_, err = ListAssets(context.Background(), "token", nil, AssetsOptions{ProxyRuntime: transportRuntime, Client: transportClient})
	if !errors.As(err, &upstream) || upstream.Status != 502 || !strings.Contains(upstream.Message, "list_assets: transport error: dial failed") {
		t.Fatalf("transport error = %T %#v", err, err)
	}
	assertAssetsFeedback(t, transportRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestDeleteAssetAppliesUpstreamAndTransportFeedback(t *testing.T) {
	upstreamRuntime := newFakeAssetsProxyRuntime("upstream-lease")
	upstreamClient := &fakeAssetsHTTPClient{err: platform.NewUpstreamError("delete denied", 403, "body")}
	_, err := DeleteAsset(context.Background(), "token", "asset-1", AssetsOptions{ProxyRuntime: upstreamRuntime, Client: upstreamClient})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 403 {
		t.Fatalf("upstream error = %T %v", err, err)
	}
	assertAssetsFeedback(t, upstreamRuntime.feedbacks, 0, controlproxy.ProxyFeedbackChallenge, intPtr(403))

	transportRuntime := newFakeAssetsProxyRuntime("transport-lease")
	transportClient := &fakeAssetsHTTPClient{err: errors.New("delete failed")}
	_, err = DeleteAsset(context.Background(), "token", "asset-1", AssetsOptions{ProxyRuntime: transportRuntime, Client: transportClient})
	if !errors.As(err, &upstream) || upstream.Status != 502 || !strings.Contains(upstream.Message, "delete_asset: transport error: delete failed") {
		t.Fatalf("transport error = %T %#v", err, err)
	}
	assertAssetsFeedback(t, transportRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestDownloadAssetAppliesUpstreamAndTransportFeedback(t *testing.T) {
	upstreamRuntime := newFakeAssetsProxyRuntime("upstream-lease")
	upstreamClient := &fakeAssetsHTTPClient{err: platform.NewUpstreamError("download rate limited", 429, "body")}
	_, err := DownloadAsset(context.Background(), "token", "users/u/file.png", AssetsOptions{ProxyRuntime: upstreamRuntime, Client: upstreamClient})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 429 {
		t.Fatalf("upstream error = %T %v", err, err)
	}
	assertAssetsFeedback(t, upstreamRuntime.feedbacks, 0, controlproxy.ProxyFeedbackRateLimited, intPtr(429))

	transportRuntime := newFakeAssetsProxyRuntime("transport-lease")
	transportClient := &fakeAssetsHTTPClient{err: errors.New("stream failed")}
	_, err = DownloadAsset(context.Background(), "token", "users/u/file.png", AssetsOptions{ProxyRuntime: transportRuntime, Client: transportClient})
	if !errors.As(err, &upstream) || upstream.Status != 502 || !strings.Contains(upstream.Message, "download_asset: transport error: stream failed") {
		t.Fatalf("transport error = %T %#v", err, err)
	}
	assertAssetsFeedback(t, transportRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func assertAssetsAcquire(t *testing.T, acquire controlproxy.AcquireOptions, scope controlproxy.ProxyScope, kind controlproxy.RequestKind) {
	t.Helper()
	if acquire.Scope != scope || acquire.Kind != kind {
		t.Fatalf("acquire options = %#v, want %s/%s", acquire, scope, kind)
	}
}

func assertAssetsFeedback(t *testing.T, feedbacks []assetsFeedbackCall, index int, kind controlproxy.ProxyFeedbackKind, status *int) {
	t.Helper()
	if len(feedbacks) <= index {
		t.Fatalf("feedbacks = %#v, missing index %d", feedbacks, index)
	}
	feedback := feedbacks[index].feedback
	if feedback.Kind != kind {
		t.Fatalf("feedback kind = %q, want %q", feedback.Kind, kind)
	}
	if status == nil {
		if feedback.StatusCode != nil {
			t.Fatalf("status = %#v, want nil", feedback.StatusCode)
		}
		return
	}
	if feedback.StatusCode == nil || *feedback.StatusCode != *status {
		t.Fatalf("status = %#v, want %d", feedback.StatusCode, *status)
	}
}

func resetAssetsConcurrencyForTest(t *testing.T, listValue int, deleteValue int) {
	t.Helper()
	assetsSlotsMu.Lock()
	originalListProvider := assetListConcurrencyProvider
	originalDeleteProvider := assetDeleteConcurrencyProvider
	originalListSlots := assetListSlots
	originalDeleteSlots := assetDeleteSlots
	assetListConcurrencyProvider = func() int { return listValue }
	assetDeleteConcurrencyProvider = func() int { return deleteValue }
	assetListSlots = nil
	assetDeleteSlots = nil
	assetsSlotsMu.Unlock()

	t.Cleanup(func() {
		assetsSlotsMu.Lock()
		assetListConcurrencyProvider = originalListProvider
		assetDeleteConcurrencyProvider = originalDeleteProvider
		assetListSlots = originalListSlots
		assetDeleteSlots = originalDeleteSlots
		assetsSlotsMu.Unlock()
	})
}

func resetAssetsTimeoutsForTest(t *testing.T, listValue float64, deleteValue float64, downloadValue float64) {
	t.Helper()
	originalListProvider := assetListTimeoutProvider
	originalDeleteProvider := assetDeleteTimeoutProvider
	originalDownloadProvider := assetDownloadTimeoutProvider
	assetListTimeoutProvider = func() float64 { return listValue }
	assetDeleteTimeoutProvider = func() float64 { return deleteValue }
	assetDownloadTimeoutProvider = func() float64 { return downloadValue }
	t.Cleanup(func() {
		assetListTimeoutProvider = originalListProvider
		assetDeleteTimeoutProvider = originalDeleteProvider
		assetDownloadTimeoutProvider = originalDownloadProvider
	})
}

func newFakeAssetsProxyRuntime(ids ...string) *fakeAssetsProxyRuntime {
	leases := make([]controlproxy.ProxyLease, 0, len(ids))
	for _, id := range ids {
		lease := controlproxy.NewProxyLease(id)
		leases = append(leases, lease)
	}
	return &fakeAssetsProxyRuntime{leases: leases}
}

type fakeAssetsProxyRuntime struct {
	leases    []controlproxy.ProxyLease
	acquires  []controlproxy.AcquireOptions
	feedbacks []assetsFeedbackCall
}

func (r *fakeAssetsProxyRuntime) Acquire(_ context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	option := controlproxy.AcquireOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	r.acquires = append(r.acquires, option)
	if len(r.leases) == 0 {
		return controlproxy.NewProxyLease("lease"), nil
	}
	lease := r.leases[0]
	r.leases = r.leases[1:]
	return lease, nil
}

func (r *fakeAssetsProxyRuntime) Feedback(_ context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedbacks = append(r.feedbacks, assetsFeedbackCall{lease: lease, feedback: feedback})
	return nil
}

type assetsFeedbackCall struct {
	lease    controlproxy.ProxyLease
	feedback controlproxy.ProxyFeedback
}

type fakeAssetsHTTPClient struct {
	jsonResult map[string]any
	stream     io.ReadCloser
	err        error
	getJSON    []AssetsHTTPRequest
	deleteJSON []AssetsHTTPRequest
	streams    []AssetsHTTPRequest
}

func (c *fakeAssetsHTTPClient) GetJSON(_ context.Context, request AssetsHTTPRequest) (map[string]any, error) {
	c.getJSON = append(c.getJSON, request)
	return c.jsonResult, c.err
}

func (c *fakeAssetsHTTPClient) DeleteJSON(_ context.Context, request AssetsHTTPRequest) (map[string]any, error) {
	c.deleteJSON = append(c.deleteJSON, request)
	return c.jsonResult, c.err
}

func (c *fakeAssetsHTTPClient) GetBytesStream(_ context.Context, request AssetsHTTPRequest) (io.ReadCloser, error) {
	c.streams = append(c.streams, request)
	return c.stream, c.err
}
