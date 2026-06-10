package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestParseDataURIMatchesPython(t *testing.T) {
	filename, content, mime, err := ParseDataURI("data:image/png;base64, aGk= \n")
	if err != nil {
		t.Fatalf("ParseDataURI returned error: %v", err)
	}
	if filename != "file.png" || content != "aGk=" || mime != "image/png" {
		t.Fatalf("parsed data uri = %q/%q/%q", filename, content, mime)
	}
}

func TestParseDataURIRejectsInvalidInputLikePython(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		message string
	}{
		{name: "not data URI", input: "https://example.test/file.png", message: "File input must be a URL or data URI"},
		{name: "missing comma", input: "data:image/png;base64", message: "Malformed data URI: missing comma separator"},
		{name: "not base64", input: "data:text/plain,abc", message: "Data URI must be base64-encoded"},
		{name: "empty payload", input: "data:text/plain;base64, \n", message: "Data URI has empty payload"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := ParseDataURI(tt.input)
			var validation *platform.ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T %v, want ValidationError", err, err)
			}
			if validation.Message != tt.message || validation.Param != "content" {
				t.Fatalf("validation error = %#v", validation)
			}
		})
	}
}

func TestMimeFromNameMatchesPythonFallback(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		fallback string
		want     string
	}{
		{name: "known png", filename: "file.png", fallback: "application/octet-stream", want: "image/png"},
		{name: "known text strips charset", filename: "note.txt", fallback: "application/octet-stream", want: "text/plain"},
		{name: "unknown uses fallback", filename: "file.unknownext", fallback: "application/custom", want: "application/custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mimeFromName(tt.filename, tt.fallback); got != tt.want {
				t.Fatalf("mimeFromName(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

func TestAcquireAssetUploadSlotUsesConfiguredConcurrencyLikePython(t *testing.T) {
	resetAssetUploadConcurrencyForTest(t, 2)

	release1, err := acquireAssetUploadSlot(context.Background())
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}
	defer release1()

	release2, err := acquireAssetUploadSlot(context.Background())
	if err != nil {
		t.Fatalf("second acquire returned error: %v", err)
	}
	defer release2()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if release3, err := acquireAssetUploadSlot(ctx); err == nil {
		release3()
		t.Fatal("third acquire succeeded with configured concurrency 2")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("third acquire error = %v, want deadline exceeded", err)
	}
}

func TestAssetUploadConcurrencyFloorsInvalidConfigLikePython(t *testing.T) {
	for _, value := range []int{0, -3} {
		resetAssetUploadConcurrencyForTest(t, value)
		if got := cap(assetUploadSlotChannel()); got != 1 {
			t.Fatalf("configured concurrency %d produced channel cap %d, want 1", value, got)
		}
	}
}

func TestAssetUploadOptionsUsesConfiguredUploadTimeoutLikePython(t *testing.T) {
	resetAssetUploadTimeoutForTest(t, 2.5)

	option := assetUploadOptions()
	if option.UploadTimeout != 2500*time.Millisecond {
		t.Fatalf("upload timeout = %s, want 2.5s", option.UploadTimeout)
	}

	option = assetUploadOptions(AssetUploadOptions{UploadTimeout: 12 * time.Second})
	if option.UploadTimeout != 12*time.Second {
		t.Fatalf("explicit upload timeout = %s, want 12s", option.UploadTimeout)
	}
}

func TestUploadFilePostsPayloadAndAppliesSuccessFeedback(t *testing.T) {
	proxyRuntime := newFakeAssetProxyRuntime("lease-1")
	client := &fakeAssetHTTPClient{
		postResponses: []AssetHTTPResponse{{
			StatusCode: 200,
			Body:       []byte(`{"fileMetadataId":"meta-1","fileId":"fallback","fileUri":"uploads/file.png"}`),
		}},
	}

	result, err := UploadFile(context.Background(), "sso-token", "file.png", "image/png", "aGk=", AssetUploadOptions{
		ProxyRuntime:  proxyRuntime,
		Client:        client,
		UploadTimeout: 12 * time.Second,
	})
	if err != nil {
		t.Fatalf("UploadFile returned error: %v", err)
	}
	if result.FileID != "meta-1" || result.FileURI != "uploads/file.png" {
		t.Fatalf("upload result = %#v", result)
	}
	if len(client.posts) != 1 {
		t.Fatalf("post calls = %#v", client.posts)
	}
	post := client.posts[0]
	if post.url != AssetUploadURL || post.timeout != 12*time.Second {
		t.Fatalf("post target = %#v", post)
	}
	if !strings.Contains(post.headers["Cookie"], "sso=sso-token") {
		t.Fatalf("headers did not include SSO cookie: %#v", post.headers)
	}
	var payload map[string]string
	if err := json.Unmarshal(post.body, &payload); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if payload["fileName"] != "file.png" || payload["fileMimeType"] != "image/png" || payload["content"] != "aGk=" {
		t.Fatalf("payload = %#v", payload)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, intPtr(200))
}

func TestUploadFileClassifiesHTTPFailureAndTruncatesBody(t *testing.T) {
	body := "Just a Moment " + strings.Repeat("x", 400)
	proxyRuntime := newFakeAssetProxyRuntime("lease-1")
	client := &fakeAssetHTTPClient{
		postResponses: []AssetHTTPResponse{{StatusCode: 403, Body: []byte(body)}},
	}

	_, err := UploadFile(context.Background(), "token", "file.png", "image/png", "aGk=", AssetUploadOptions{
		ProxyRuntime: proxyRuntime,
		Client:       client,
	})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 403 || upstream.Message != "Asset upload returned 403" {
		t.Fatalf("upstream error = %#v", upstream)
	}
	if len(upstream.Body) != 300 || !strings.HasPrefix(upstream.Body, "Just a Moment") {
		t.Fatalf("upstream body length/prefix = %d/%q", len(upstream.Body), upstream.Body)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackChallenge, intPtr(403))
}

func TestUploadFileReportsTransportErrorLikePython(t *testing.T) {
	proxyRuntime := newFakeAssetProxyRuntime("lease-1")
	client := &fakeAssetHTTPClient{postErr: errors.New("dial failed")}

	_, err := UploadFile(context.Background(), "token", "file.png", "image/png", "aGk=", AssetUploadOptions{
		ProxyRuntime: proxyRuntime,
		Client:       client,
	})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || !strings.Contains(upstream.Message, "Asset upload transport error: dial failed") {
		t.Fatalf("error = %#v, want upload transport UpstreamError", err)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestUploadFileReportsParseErrorAfterSuccessFeedbackLikePython(t *testing.T) {
	proxyRuntime := newFakeAssetProxyRuntime("lease-1")
	client := &fakeAssetHTTPClient{
		postResponses: []AssetHTTPResponse{{StatusCode: 200, Body: []byte(`not-json`)}},
	}

	_, err := UploadFile(context.Background(), "token", "file.png", "image/png", "aGk=", AssetUploadOptions{
		ProxyRuntime: proxyRuntime,
		Client:       client,
	})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || !strings.Contains(upstream.Message, "Asset upload transport error") {
		t.Fatalf("error = %#v, want parse transport UpstreamError", err)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, intPtr(200))
	assertAssetFeedback(t, proxyRuntime.feedbacks, 1, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestUploadFromInputFetchesURLAndReuploadsBase64(t *testing.T) {
	proxyRuntime := newFakeAssetProxyRuntime("fetch-lease", "upload-lease")
	client := &fakeAssetHTTPClient{
		getResponses: []AssetHTTPResponse{{
			StatusCode: 200,
			Body:       []byte("raw"),
			Headers:    map[string]string{"content-type": "image/jpeg; charset=utf-8"},
		}},
		postResponses: []AssetHTTPResponse{{StatusCode: 200, Body: []byte(`{"fileId":"file-1","fileUri":"uri-1"}`)}},
	}

	result, err := UploadFromInput(context.Background(), "token", "https://files.test/path/cat.jpg?download=1", AssetUploadOptions{
		ProxyRuntime: proxyRuntime,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("UploadFromInput returned error: %v", err)
	}
	if result.FileID != "file-1" || result.FileURI != "uri-1" {
		t.Fatalf("upload result = %#v", result)
	}
	if len(client.gets) != 1 || client.gets[0].url != "https://files.test/path/cat.jpg?download=1" || client.gets[0].timeout != 30*time.Second {
		t.Fatalf("get calls = %#v", client.gets)
	}
	var payload map[string]string
	if err := json.Unmarshal(client.posts[0].body, &payload); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if payload["fileName"] != "cat.jpg" || payload["fileMimeType"] != "image/jpeg" || payload["content"] != "cmF3" {
		t.Fatalf("reupload payload = %#v", payload)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, nil)
	assertAssetFeedback(t, proxyRuntime.feedbacks, 1, controlproxy.ProxyFeedbackSuccess, intPtr(200))
}

func TestUploadFromInputParsesDataURIAndUploadsLikePython(t *testing.T) {
	proxyRuntime := newFakeAssetProxyRuntime("upload-lease")
	client := &fakeAssetHTTPClient{
		postResponses: []AssetHTTPResponse{{StatusCode: 200, Body: []byte(`{"fileId":"file-1","fileUri":"uri-1"}`)}},
	}

	result, err := UploadFromInput(context.Background(), "token", "data:text/plain;base64,aGk=", AssetUploadOptions{
		ProxyRuntime: proxyRuntime,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("UploadFromInput returned error: %v", err)
	}
	if result.FileID != "file-1" || result.FileURI != "uri-1" {
		t.Fatalf("upload result = %#v", result)
	}
	if len(client.gets) != 0 || len(client.posts) != 1 {
		t.Fatalf("http calls gets=%#v posts=%#v", client.gets, client.posts)
	}
	var payload map[string]string
	if err := json.Unmarshal(client.posts[0].body, &payload); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if payload["fileName"] != "file.plain" || payload["fileMimeType"] != "text/plain" || payload["content"] != "aGk=" {
		t.Fatalf("payload = %#v", payload)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackSuccess, intPtr(200))
}

func TestUploadFromInputClassifiesFetchHTTPFailureLikePython(t *testing.T) {
	tests := []struct {
		status int
		want   controlproxy.ProxyFeedbackKind
	}{
		{status: 403, want: controlproxy.ProxyFeedbackForbidden},
		{status: 500, want: controlproxy.ProxyFeedbackUpstream5xx},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status %d", tt.status), func(t *testing.T) {
			proxyRuntime := newFakeAssetProxyRuntime("fetch-lease")
			client := &fakeAssetHTTPClient{getResponses: []AssetHTTPResponse{{StatusCode: tt.status}}}

			_, err := UploadFromInput(context.Background(), "token", "https://files.test/path/cat.jpg", AssetUploadOptions{
				ProxyRuntime: proxyRuntime,
				Client:       client,
			})
			var upstream *platform.UpstreamError
			if !errors.As(err, &upstream) || upstream.Status != tt.status || upstream.Message != fmt.Sprintf("Failed to fetch input URL: %d", tt.status) {
				t.Fatalf("error = %#v, want fetch status %d", err, tt.status)
			}
			if len(client.posts) != 0 {
				t.Fatalf("post calls = %#v, want none after fetch failure", client.posts)
			}
			assertAssetFeedback(t, proxyRuntime.feedbacks, 0, tt.want, intPtr(tt.status))
		})
	}
}

func TestUploadFromInputReportsFetchTransportErrorLikePython(t *testing.T) {
	proxyRuntime := newFakeAssetProxyRuntime("fetch-lease")
	client := &fakeAssetHTTPClient{getErr: errors.New("fetch failed")}

	_, err := UploadFromInput(context.Background(), "token", "https://files.test/path/cat.jpg", AssetUploadOptions{
		ProxyRuntime: proxyRuntime,
		Client:       client,
	})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || !strings.Contains(upstream.Message, "Asset fetch transport error: fetch failed") {
		t.Fatalf("error = %#v, want fetch transport UpstreamError", err)
	}
	assertAssetFeedback(t, proxyRuntime.feedbacks, 0, controlproxy.ProxyFeedbackTransportError, nil)
}

func TestResolveUploadedAssetReferenceMatchesPython(t *testing.T) {
	got, err := ResolveUploadedAssetReference("sso=abc; x-userid=user-1", "file-1", "")
	if err != nil {
		t.Fatalf("ResolveUploadedAssetReference returned error: %v", err)
	}
	if got != "https://assets.grok.com/users/user-1/file-1/content" {
		t.Fatalf("resolved URL = %q", got)
	}

	got, err = ResolveUploadedAssetReference("token", "file-1", "uploads/file.png")
	if err != nil {
		t.Fatalf("ResolveUploadedAssetReference with URI returned error: %v", err)
	}
	if got != "https://assets.grok.com/uploads/file.png" {
		t.Fatalf("resolved URI URL = %q", got)
	}

	_, err = ResolveUploadedAssetReference("token", "file-1", "")
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Message != "Could not resolve uploaded asset reference URL" {
		t.Fatalf("missing user error = %T %v", err, err)
	}
}

func assertAssetFeedback(t *testing.T, feedbacks []assetFeedbackCall, index int, kind controlproxy.ProxyFeedbackKind, status *int) {
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
			t.Fatalf("feedback status = %#v, want nil", feedback.StatusCode)
		}
		return
	}
	if feedback.StatusCode == nil || *feedback.StatusCode != *status {
		t.Fatalf("feedback status = %#v, want %d", feedback.StatusCode, *status)
	}
}

func resetAssetUploadConcurrencyForTest(t *testing.T, value int) {
	t.Helper()
	assetUploadSlotsMu.Lock()
	originalProvider := assetUploadConcurrencyProvider
	originalSlots := assetUploadSlots
	assetUploadConcurrencyProvider = func() int { return value }
	assetUploadSlots = nil
	assetUploadSlotsMu.Unlock()

	t.Cleanup(func() {
		assetUploadSlotsMu.Lock()
		assetUploadConcurrencyProvider = originalProvider
		assetUploadSlots = originalSlots
		assetUploadSlotsMu.Unlock()
	})
}

func resetAssetUploadTimeoutForTest(t *testing.T, value float64) {
	t.Helper()
	originalProvider := assetUploadTimeoutProvider
	assetUploadTimeoutProvider = func() float64 { return value }
	t.Cleanup(func() {
		assetUploadTimeoutProvider = originalProvider
	})
}

func newFakeAssetProxyRuntime(ids ...string) *fakeAssetProxyRuntime {
	leases := make([]controlproxy.ProxyLease, 0, len(ids))
	for _, id := range ids {
		lease := controlproxy.NewProxyLease(id)
		leases = append(leases, lease)
	}
	return &fakeAssetProxyRuntime{leases: leases}
}

type fakeAssetProxyRuntime struct {
	leases    []controlproxy.ProxyLease
	acquires  int
	feedbacks []assetFeedbackCall
}

func (r *fakeAssetProxyRuntime) Acquire(context.Context) (*controlproxy.ProxyLease, error) {
	if r.acquires >= len(r.leases) {
		lease := controlproxy.NewProxyLease("lease")
		r.leases = append(r.leases, lease)
	}
	lease := r.leases[r.acquires]
	r.acquires++
	return &lease, nil
}

func (r *fakeAssetProxyRuntime) Feedback(_ context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	r.feedbacks = append(r.feedbacks, assetFeedbackCall{lease: lease, feedback: feedback})
	return nil
}

type assetFeedbackCall struct {
	lease    controlproxy.ProxyLease
	feedback controlproxy.ProxyFeedback
}

type fakeAssetHTTPClient struct {
	postResponses []AssetHTTPResponse
	getResponses  []AssetHTTPResponse
	postErr       error
	getErr        error
	posts         []assetHTTPCall
	gets          []assetHTTPCall
}

func (c *fakeAssetHTTPClient) Post(_ context.Context, url string, headers map[string]string, body []byte, timeout time.Duration) (AssetHTTPResponse, error) {
	c.posts = append(c.posts, assetHTTPCall{url: url, headers: headers, body: append([]byte(nil), body...), timeout: timeout})
	if c.postErr != nil {
		return AssetHTTPResponse{}, c.postErr
	}
	if len(c.postResponses) == 0 {
		return AssetHTTPResponse{StatusCode: 200}, nil
	}
	response := c.postResponses[0]
	c.postResponses = c.postResponses[1:]
	return response, nil
}

func (c *fakeAssetHTTPClient) Get(_ context.Context, url string, headers map[string]string, timeout time.Duration) (AssetHTTPResponse, error) {
	c.gets = append(c.gets, assetHTTPCall{url: url, headers: headers, timeout: timeout})
	if c.getErr != nil {
		return AssetHTTPResponse{}, c.getErr
	}
	if len(c.getResponses) == 0 {
		return AssetHTTPResponse{StatusCode: 200}, nil
	}
	response := c.getResponses[0]
	c.getResponses = c.getResponses[1:]
	return response, nil
}

type assetHTTPCall struct {
	url     string
	headers map[string]string
	body    []byte
	timeout time.Duration
}

func intPtr(value int) *int {
	return &value
}
