package admin

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
)

func TestDefaultAdminBatchNSFWSequenceUsesHTTPTransport(t *testing.T) {
	roundTripper := &fakeAdminBatchAuthRoundTripper{}
	oldTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripper
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })

	err := defaultAdminBatchNSFWSequence(context.Background(), "tok")
	if err != nil {
		t.Fatalf("defaultAdminBatchNSFWSequence returned error: %v", err)
	}
	if roundTripper.grpcCalls != 2 || roundTripper.jsonCalls != 1 {
		t.Fatalf("calls grpc/json=%d/%d", roundTripper.grpcCalls, roundTripper.jsonCalls)
	}
	if len(roundTripper.requests) != 3 {
		t.Fatalf("requests = %#v", roundTripper.requests)
	}
	assertAdminBatchAuthRequest(t, roundTripper.requests[0], protocol.AcceptTOSURL, "application/grpc-web+proto", protocol.AccountsOrigin, protocol.AccountsOrigin+"/accept-tos")
	if !bytes.Equal(roundTripper.requests[0].Body, protocol.BuildAcceptTOSPayload()) {
		t.Fatalf("accept tos body = %x", roundTripper.requests[0].Body)
	}
	assertAdminBatchAuthRequest(t, roundTripper.requests[1], protocol.SetBirthURL, "application/json", protocol.GrokOrigin, protocol.GrokOrigin+"/?_s=data")
	if !strings.Contains(string(roundTripper.requests[1].Body), "birthDate") {
		t.Fatalf("birth body = %s", roundTripper.requests[1].Body)
	}
	assertAdminBatchAuthRequest(t, roundTripper.requests[2], protocol.NSFWMgmtURL, "application/grpc-web+proto", protocol.GrokOrigin, protocol.GrokOrigin+"/?_s=data")
	if !bytes.Equal(roundTripper.requests[2].Body, protocol.BuildNSFWMgmtPayload(true)) {
		t.Fatalf("enable nsfw body = %x", roundTripper.requests[2].Body)
	}
}

func TestDefaultAdminBatchSetNSFWUsesHTTPTransport(t *testing.T) {
	roundTripper := &fakeAdminBatchAuthRoundTripper{}
	oldTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripper
	t.Cleanup(func() { http.DefaultClient.Transport = oldTransport })

	err := defaultAdminBatchSetNSFW(context.Background(), "tok", false)
	if err != nil {
		t.Fatalf("defaultAdminBatchSetNSFW returned error: %v", err)
	}
	if roundTripper.grpcCalls != 1 || roundTripper.jsonCalls != 0 {
		t.Fatalf("calls grpc/json=%d/%d", roundTripper.grpcCalls, roundTripper.jsonCalls)
	}
	if len(roundTripper.requests) != 1 {
		t.Fatalf("requests = %#v", roundTripper.requests)
	}
	assertAdminBatchAuthRequest(t, roundTripper.requests[0], protocol.NSFWMgmtURL, "application/grpc-web+proto", protocol.GrokOrigin, protocol.GrokOrigin+"/?_s=data")
	if !bytes.Equal(roundTripper.requests[0].Body, protocol.BuildNSFWMgmtPayload(false)) {
		t.Fatalf("disable nsfw body = %x", roundTripper.requests[0].Body)
	}
}

type adminBatchAuthCapturedRequest struct {
	URL         string
	Body        []byte
	Cookie      string
	ContentType string
	Origin      string
	Referer     string
	XGRPCWeb    string
	XUserAgent  string
}

type fakeAdminBatchAuthRoundTripper struct {
	grpcCalls int
	jsonCalls int
	requests  []adminBatchAuthCapturedRequest
}

func (rt *fakeAdminBatchAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	contentType := req.Header.Get("Content-Type")
	rt.requests = append(rt.requests, adminBatchAuthCapturedRequest{
		URL:         req.URL.String(),
		Body:        append([]byte(nil), body...),
		Cookie:      req.Header.Get("Cookie"),
		ContentType: contentType,
		Origin:      req.Header.Get("Origin"),
		Referer:     req.Header.Get("Referer"),
		XGRPCWeb:    req.Header.Get("x-grpc-web"),
		XUserAgent:  req.Header.Get("x-user-agent"),
	})
	switch {
	case strings.Contains(contentType, "application/grpc-web"):
		rt.grpcCalls++
		if len(body) == 0 {
			return adminBatchAuthHTTPResponse(http.StatusBadRequest, "text/plain", "missing grpc body"), nil
		}
		payload := adminBatchAuthGRPCFrame(0x80, []byte("grpc-status: 0\r\n"))
		return adminBatchAuthHTTPResponse(http.StatusOK, "application/grpc-web+proto", string(payload)), nil
	case contentType == "application/json":
		rt.jsonCalls++
		return adminBatchAuthHTTPResponse(http.StatusOK, "application/json", `{}`), nil
	default:
		return adminBatchAuthHTTPResponse(http.StatusUnsupportedMediaType, "text/plain", contentType), nil
	}
}

func adminBatchAuthHTTPResponse(status int, contentType string, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func adminBatchAuthGRPCFrame(flag byte, payload []byte) []byte {
	frame := bytes.NewBuffer(make([]byte, 0, len(payload)+5))
	frame.WriteByte(flag)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	frame.Write(length[:])
	frame.Write(payload)
	return frame.Bytes()
}

func assertAdminBatchAuthRequest(t *testing.T, request adminBatchAuthCapturedRequest, url, contentType, origin, referer string) {
	t.Helper()
	if request.URL != url || request.ContentType != contentType || request.Origin != origin || request.Referer != referer {
		t.Fatalf("request = %#v", request)
	}
	if !strings.Contains(request.Cookie, "sso=tok; sso-rw=tok") {
		t.Fatalf("cookie = %q", request.Cookie)
	}
	if strings.Contains(contentType, "application/grpc-web") && (request.XGRPCWeb != "1" || request.XUserAgent != "connect-es/2.1.1") {
		t.Fatalf("grpc-web headers = %#v", request)
	}
}
