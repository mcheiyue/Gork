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
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestPostJSONBuildsHeadersAndParsesAllowedStatuses(t *testing.T) {
	lease := controlproxy.NewProxyLease("http-lease")
	client := &fakeHTTPClient{responses: []HTTPResponse{
		{StatusCode: 201, Body: []byte(`{"ok":true}`)},
		{StatusCode: 204, Body: []byte(" \n")},
	}}

	result, err := PostJSON(context.Background(), "https://grok.test/json", "token", []byte(`{"q":1}`), HTTPOptions{
		Client:      client,
		Lease:       &lease,
		Timeout:     3 * time.Second,
		ContentType: "application/custom",
		Origin:      "https://origin.test",
		Referer:     "https://referer.test/",
	})
	if err != nil {
		t.Fatalf("PostJSON returned error: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("post result = %#v", result)
	}
	request := client.posts[0]
	if request.URL != "https://grok.test/json" || string(request.Payload) != `{"q":1}` || request.Timeout != 3*time.Second {
		t.Fatalf("post request = %#v", request)
	}
	if request.Headers["Content-Type"] != "application/custom" ||
		request.Headers["Origin"] != "https://origin.test" ||
		request.Headers["Referer"] != "https://referer.test/" ||
		!strings.Contains(request.Headers["Cookie"], "sso=token") {
		t.Fatalf("headers = %#v", request.Headers)
	}

	empty, err := PostJSON(context.Background(), "https://grok.test/empty", "token", nil, HTTPOptions{Client: client})
	if err != nil {
		t.Fatalf("PostJSON empty returned error: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty body result = %#v", empty)
	}
}

func TestPostJSONFailureReturnsUpstreamErrorWithTruncatedBody(t *testing.T) {
	client := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 500, Body: []byte(strings.Repeat("x", 500))}}}
	_, err := PostJSON(context.Background(), "https://grok.test/json", "token", nil, HTTPOptions{Client: client})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 500 || upstream.Message != "Upstream returned 500" || len(upstream.Body) != 400 {
		t.Fatalf("upstream error = %#v", upstream)
	}
}

func TestHTTPTransportErrorMatchesResettableSessionWrapper(t *testing.T) {
	client := &fakeHTTPClient{err: errors.New("dial\nfailed")}
	_, err := PostJSON(context.Background(), "https://grok.test/json", "token", nil, HTTPOptions{Client: client})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 502 ||
		!strings.Contains(upstream.Message, "Transport request failed: dial") ||
		upstream.Body != "dial\\nfailed" {
		t.Fatalf("transport upstream error = %#v", upstream)
	}

	original := platform.NewUpstreamError("already upstream", 429, "body")
	client = &fakeHTTPClient{err: original}
	_, err = PostJSON(context.Background(), "https://grok.test/json", "token", nil, HTTPOptions{Client: client})
	if !errors.As(err, &upstream) || upstream != original {
		t.Fatalf("upstream passthrough error = %#v, want original", err)
	}
}

func TestHTTPDefaultOptionsMatchPython(t *testing.T) {
	postJSONClient := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 200, Body: []byte(`{"ok":true}`)}}}
	_, err := PostJSON(context.Background(), "https://grok.test/json", "token", []byte(`{"q":1}`), HTTPOptions{Client: postJSONClient})
	if err != nil {
		t.Fatalf("PostJSON returned error: %v", err)
	}
	postJSONRequest := postJSONClient.posts[0]
	if postJSONRequest.Timeout != 30*time.Second ||
		postJSONRequest.Headers["Content-Type"] != "application/json" ||
		postJSONRequest.Headers["Origin"] != "https://grok.com" ||
		postJSONRequest.Headers["Referer"] != "https://grok.com/" {
		t.Fatalf("PostJSON defaults = %#v headers=%#v", postJSONRequest, postJSONRequest.Headers)
	}

	postStreamClient := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 200, Stream: &trackingReadCloser{Reader: strings.NewReader("data: ok\n")}}}}
	lines, err := PostStream(context.Background(), "https://grok.test/stream", "token", []byte("payload"), HTTPOptions{Client: postStreamClient})
	if err != nil {
		t.Fatalf("PostStream returned error: %v", err)
	}
	_ = drainHTTPLineStream(t, lines)
	postStreamRequest := postStreamClient.posts[0]
	if postStreamRequest.Timeout != 120*time.Second ||
		postStreamRequest.Headers["Content-Type"] != "application/json" ||
		!postStreamRequest.Stream {
		t.Fatalf("PostStream defaults = %#v headers=%#v", postStreamRequest, postStreamRequest.Headers)
	}

	bytesClient := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 200, Stream: io.NopCloser(strings.NewReader("bytes"))}}}
	stream, err := GetBytesStream(context.Background(), "https://assets.test/file.png", "token", HTTPOptions{Client: bytesClient})
	if err != nil {
		t.Fatalf("GetBytesStream returned error: %v", err)
	}
	_ = stream.Close()
	bytesRequest := bytesClient.gets[0]
	if bytesRequest.Timeout != 120*time.Second ||
		bytesRequest.Headers["Origin"] != "https://assets.grok.com" ||
		bytesRequest.Headers["Referer"] != "https://grok.com/" ||
		!bytesRequest.Stream || !bytesRequest.AllowRedirects {
		t.Fatalf("GetBytesStream defaults = %#v headers=%#v", bytesRequest, bytesRequest.Headers)
	}
}

func TestGetJSONAndDeleteJSONUseParamsAndAllowedStatuses(t *testing.T) {
	client := &fakeHTTPClient{responses: []HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"items":[1]}`)},
		{StatusCode: 204, Body: nil},
	}}

	got, err := GetJSON(context.Background(), "https://grok.test/assets", "token", HTTPOptions{
		Client: client,
		Params: map[string]any{"limit": 50},
	})
	if err != nil {
		t.Fatalf("GetJSON returned error: %v", err)
	}
	if !reflect.DeepEqual(got, map[string]any{"items": []any{float64(1)}}) {
		t.Fatalf("get result = %#v", got)
	}
	if !reflect.DeepEqual(client.gets[0].Params, map[string]any{"limit": 50}) {
		t.Fatalf("get params = %#v", client.gets[0].Params)
	}

	deleted, err := DeleteJSON(context.Background(), "https://grok.test/assets/id", "token", HTTPOptions{Client: client})
	if err != nil {
		t.Fatalf("DeleteJSON returned error: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("delete empty result = %#v", deleted)
	}
}

func TestGetJSONEmptyBodyMatchesPythonDecodeError(t *testing.T) {
	client := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 200, Body: []byte(" \n")}}}
	_, err := GetJSON(context.Background(), "https://grok.test/assets", "token", HTTPOptions{Client: client})
	if err == nil {
		t.Fatalf("GetJSON empty body should return a JSON decode error")
	}
}

func TestPostStreamYieldsLinesAndCloses(t *testing.T) {
	stream := &trackingReadCloser{Reader: strings.NewReader("data: one\n\ndata: two\n")}
	client := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 200, Stream: stream}}}

	lines, err := PostStream(context.Background(), "https://grok.test/stream", "token", []byte("payload"), HTTPOptions{
		Client:  client,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("PostStream returned error: %v", err)
	}
	got := drainHTTPLineStream(t, lines)
	if !reflect.DeepEqual(got, []string{"data: one", "", "data: two"}) {
		t.Fatalf("stream lines = %#v", got)
	}
	if !stream.closed {
		t.Fatalf("line stream did not close underlying response")
	}
	if !client.posts[0].Stream {
		t.Fatalf("post stream request should be marked streaming: %#v", client.posts[0])
	}
}

func TestPostStreamFailureClosesResponseAndTruncatesBody(t *testing.T) {
	stream := &trackingReadCloser{Reader: strings.NewReader("ignored")}
	client := &fakeHTTPClient{responses: []HTTPResponse{{
		StatusCode: 503,
		Body:       []byte(strings.Repeat("x", 500)),
		Stream:     stream,
	}}}

	_, err := PostStream(context.Background(), "https://grok.test/stream", "token", []byte("payload"), HTTPOptions{Client: client})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 503 || upstream.Message != "Upstream returned 503" || len(upstream.Body) != 400 {
		t.Fatalf("upstream error = %#v", upstream)
	}
	if !stream.closed {
		t.Fatal("non-200 stream response was not closed")
	}
}

func TestGetBytesStreamRemovesNavigateContentHeaders(t *testing.T) {
	stream := &trackingReadCloser{Reader: strings.NewReader("bytes")}
	client := &fakeHTTPClient{responses: []HTTPResponse{{StatusCode: 200, Stream: stream}}}

	result, err := GetBytesStream(context.Background(), "https://assets.test/file.png", "token", HTTPOptions{
		Client: client,
		ExtraHeaders: map[string]string{
			"Sec-Fetch-Mode": "navigate",
			"Accept":         "image/*",
		},
		Timeout: 6 * time.Second,
		Origin:  "https://assets.test",
		Referer: "https://assets.test/",
	})
	if err != nil {
		t.Fatalf("GetBytesStream returned error: %v", err)
	}
	body, err := io.ReadAll(result)
	if err != nil {
		t.Fatalf("read bytes stream: %v", err)
	}
	if string(body) != "bytes" {
		t.Fatalf("body = %q", body)
	}
	request := client.gets[0]
	if request.Headers["Content-Type"] != "" || request.Headers["Origin"] != "" {
		t.Fatalf("navigate headers should remove content type and origin: %#v", request.Headers)
	}
	if request.Headers["Accept"] != "image/*" || !request.Stream || !request.AllowRedirects || request.Timeout != 6*time.Second {
		t.Fatalf("bytes request = %#v", request)
	}
}

func drainHTTPLineStream(t *testing.T, stream *HTTPLineStream) []string {
	t.Helper()
	defer stream.Close()
	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			t.Fatalf("Next returned error: %v", err)
		}
		if !ok {
			return lines
		}
		lines = append(lines, line)
	}
}

type fakeHTTPClient struct {
	responses []HTTPResponse
	err       error
	posts     []HTTPRequest
	gets      []HTTPRequest
	deletes   []HTTPRequest
}

func (c *fakeHTTPClient) Post(_ context.Context, request HTTPRequest) (HTTPResponse, error) {
	c.posts = append(c.posts, request)
	return c.next()
}

func (c *fakeHTTPClient) Get(_ context.Context, request HTTPRequest) (HTTPResponse, error) {
	c.gets = append(c.gets, request)
	return c.next()
}

func (c *fakeHTTPClient) Delete(_ context.Context, request HTTPRequest) (HTTPResponse, error) {
	c.deletes = append(c.deletes, request)
	return c.next()
}

func (c *fakeHTTPClient) next() (HTTPResponse, error) {
	if c.err != nil {
		return HTTPResponse{}, c.err
	}
	if len(c.responses) == 0 {
		return HTTPResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
	}
	response := c.responses[0]
	c.responses = c.responses[1:]
	return response, nil
}

type trackingReadCloser struct {
	*strings.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}
