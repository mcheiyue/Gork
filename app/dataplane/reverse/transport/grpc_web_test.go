package transport

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func TestEncodeGRPCWebPayloadMatchesPythonFrame(t *testing.T) {
	got := EncodeGRPCWebPayload([]byte{0x01, 0x02, 0x03})
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x03, 0x01, 0x02, 0x03}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("encoded frame = %#v, want %#v", got, want)
	}
}

func TestParseGRPCWebResponseReadsMessagesAndTrailers(t *testing.T) {
	body := append(grpcFrame(0x00, []byte("msg")), grpcFrame(0x80, []byte("grpc-status: 0\r\ngrpc-message: ok%20done\r\n"))...)

	result, err := ParseGRPCWebResponse(body, "application/grpc-web+proto", nil)
	if err != nil {
		t.Fatalf("ParseGRPCWebResponse returned error: %v", err)
	}
	if len(result.Messages) != 1 || string(result.Messages[0]) != "msg" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if result.Trailers["grpc-status"] != "0" || result.Trailers["grpc-message"] != "ok done" {
		t.Fatalf("trailers = %#v", result.Trailers)
	}
}

func TestParseGRPCWebResponseDecodesBase64AndHeaderTrailers(t *testing.T) {
	body := []byte(base64.StdEncoding.EncodeToString(grpcFrame(0x00, []byte("msg"))))

	result, err := ParseGRPCWebResponse(body, "application/grpc-web-text+proto", map[string]string{
		"Grpc-Status":  "0",
		"Grpc-Message": "header%20ok",
	})
	if err != nil {
		t.Fatalf("ParseGRPCWebResponse returned error: %v", err)
	}
	if len(result.Messages) != 1 || string(result.Messages[0]) != "msg" {
		t.Fatalf("messages = %#v", result.Messages)
	}
	if result.Trailers["grpc-message"] != "header ok" {
		t.Fatalf("trailers = %#v", result.Trailers)
	}
}

func TestParseGRPCWebResponseRejectsInvalidBase64TextLikePython(t *testing.T) {
	_, err := ParseGRPCWebResponse([]byte("not valid!!"), "application/grpc-web-text+proto", nil)
	if err == nil {
		t.Fatal("ParseGRPCWebResponse accepted invalid grpc-web-text base64")
	}
}

func TestParseGRPCWebResponseIgnoresIncompleteFramesLikePython(t *testing.T) {
	body := append(grpcFrame(0x00, []byte("msg")), []byte{0x00, 0x00, 0x00}...)

	result, err := ParseGRPCWebResponse(body, "application/grpc-web+proto", nil)
	if err != nil {
		t.Fatalf("ParseGRPCWebResponse returned error: %v", err)
	}
	if len(result.Messages) != 1 || string(result.Messages[0]) != "msg" {
		t.Fatalf("messages = %#v", result.Messages)
	}
}

func TestParseGRPCWebResponseKeepsNonZeroStatusLikePython(t *testing.T) {
	body := grpcFrame(0x80, []byte("grpc-status: 13\r\ngrpc-message: bad%20news\r\n"))

	result, err := ParseGRPCWebResponse(body, "application/grpc-web+proto", nil)
	if err != nil {
		t.Fatalf("ParseGRPCWebResponse returned error: %v", err)
	}
	if result.Trailers["grpc-status"] != "13" || result.Trailers["grpc-message"] != "bad news" {
		t.Fatalf("trailers = %#v", result.Trailers)
	}
}

func TestParseGRPCWebResponseRejectsCompressedFrame(t *testing.T) {
	_, err := ParseGRPCWebResponse(grpcFrame(0x01, []byte("compressed")), "application/grpc-web+proto", nil)
	if err == nil || err.Error() != "grpc-web compressed frame is not supported" {
		t.Fatalf("compressed frame error = %v", err)
	}
}

func TestPostGRPCWebBuildsHeadersAndParsesResponse(t *testing.T) {
	lease := controlproxy.NewProxyLease("grpc-lease")
	client := &fakeGRPCWebHTTPClient{response: GRPCWebHTTPResponse{
		StatusCode: 200,
		Body:       append(grpcFrame(0x00, []byte("msg")), grpcFrame(0x80, []byte("grpc-status: 0\r\n"))...),
		Headers:    map[string]string{"content-type": "application/grpc-web+proto"},
	}}

	result, err := PostGRPCWeb(context.Background(), protocol.GRPCWebRequest{
		URL:      "https://grok.test/grpc",
		Token:    "token",
		Payload:  []byte("payload"),
		Lease:    &lease,
		TimeoutS: 12.5,
		Origin:   "https://origin.test",
		Referer:  "https://referer.test/",
	}, GRPCWebOptions{Client: client})
	if err != nil {
		t.Fatalf("PostGRPCWeb returned error: %v", err)
	}
	if len(result.Messages) != 1 || string(result.Messages[0]) != "msg" || result.Trailers["grpc-status"] != "0" {
		t.Fatalf("result = %#v", result)
	}
	request := client.requests[0]
	if request.URL != "https://grok.test/grpc" || string(request.Payload) != "payload" || request.TimeoutS != 12.5 {
		t.Fatalf("request = %#v", request)
	}
	if request.Headers["Content-Type"] != "application/grpc-web+proto" ||
		request.Headers["x-grpc-web"] != "1" ||
		request.Headers["x-user-agent"] != "connect-es/2.1.1" ||
		request.Headers["Origin"] != "https://origin.test" {
		t.Fatalf("headers = %#v", request.Headers)
	}
}

func TestPostGRPCWebDefaultsTimeoutLikePython(t *testing.T) {
	client := &fakeGRPCWebHTTPClient{response: GRPCWebHTTPResponse{
		StatusCode: 200,
		Body:       grpcFrame(0x00, []byte("msg")),
		Headers:    map[string]string{"content-type": "application/grpc-web+proto"},
	}}

	_, err := PostGRPCWeb(context.Background(), protocol.GRPCWebRequest{
		URL: "https://grok.test/grpc", Token: "token", Payload: []byte("payload"),
	}, GRPCWebOptions{Client: client})
	if err != nil {
		t.Fatalf("PostGRPCWeb returned error: %v", err)
	}
	if client.requests[0].TimeoutS != 30 {
		t.Fatalf("timeout = %v, want 30", client.requests[0].TimeoutS)
	}
}

func TestPostGRPCWebHTTPFailureReturnsUpstreamError(t *testing.T) {
	body := "grpc down " + strings.Repeat("x", 400)
	client := &fakeGRPCWebHTTPClient{response: GRPCWebHTTPResponse{StatusCode: 503, Body: []byte(body)}}
	_, err := PostGRPCWeb(context.Background(), protocol.GRPCWebRequest{
		URL: "https://grok.test/grpc", Token: "token", Payload: []byte("payload"),
	}, GRPCWebOptions{Client: client})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 503 || upstream.Message != "Upstream returned 503" || len(upstream.Body) != 300 || !strings.HasPrefix(upstream.Body, "grpc down") {
		t.Fatalf("upstream error = %#v", upstream)
	}
}

func TestPostGRPCWebTransportErrorMatchesResettableSessionWrapper(t *testing.T) {
	client := &fakeGRPCWebHTTPClient{err: errors.New("dial failed")}

	_, err := PostGRPCWeb(context.Background(), protocol.GRPCWebRequest{
		URL: "https://grok.test/grpc", Token: "token", Payload: []byte("payload"),
	}, GRPCWebOptions{Client: client})
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %T %v, want UpstreamError", err, err)
	}
	if upstream.Status != 502 || !strings.Contains(upstream.Message, "grpc-web post transport error: dial failed") {
		t.Fatalf("upstream error = %#v", upstream)
	}
}

func grpcFrame(flag byte, payload []byte) []byte {
	frame := []byte{flag, byte(len(payload) >> 24), byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	return append(frame, payload...)
}

type fakeGRPCWebHTTPClient struct {
	response GRPCWebHTTPResponse
	err      error
	requests []GRPCWebHTTPRequest
}

func (c *fakeGRPCWebHTTPClient) Post(_ context.Context, request GRPCWebHTTPRequest) (GRPCWebHTTPResponse, error) {
	c.requests = append(c.requests, request)
	return c.response, c.err
}
