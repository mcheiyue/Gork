package net

import (
	"encoding/base64"
	"testing"
)

func TestGRPCStatusHTTPEquivalentMatchesPythonMapping(t *testing.T) {
	cases := []struct {
		code int
		http int
		ok   bool
	}{
		{code: 0, http: 200, ok: true},
		{code: 4, http: 504},
		{code: 7, http: 403},
		{code: 8, http: 429},
		{code: 14, http: 503},
		{code: 16, http: 401},
		{code: 2, http: 502},
	}

	for _, tt := range cases {
		status := GRPCStatus{Code: tt.code}
		if status.OK() != tt.ok {
			t.Fatalf("GRPCStatus{%d}.OK() = %t, want %t", tt.code, status.OK(), tt.ok)
		}
		if status.HTTPEquivalent() != tt.http {
			t.Fatalf("GRPCStatus{%d}.HTTPEquivalent() = %d, want %d", tt.code, status.HTTPEquivalent(), tt.http)
		}
	}
}

func TestGRPCStatusFromTrailersParsesStatusAndMessage(t *testing.T) {
	status := GRPCStatusFromTrailers(map[string]string{
		"grpc-status":  "14",
		"grpc-message": " unavailable ",
	})
	if status.Code != 14 || status.Message != "unavailable" {
		t.Fatalf("status = %#v", status)
	}

	status = GRPCStatusFromTrailers(map[string]string{"grpc-status": "not-int"})
	if status.Code != -1 || status.Message != "" {
		t.Fatalf("invalid status = %#v", status)
	}
}

func TestGRPCClientEncodePayloadMatchesPythonFrame(t *testing.T) {
	got := (GRPCClient{}).EncodePayload([]byte("hello"))
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'}
	if string(got) != string(want) {
		t.Fatalf("EncodePayload() = %v, want %v", got, want)
	}
}

func TestGRPCClientParseResponseDecodesMessagesAndTrailers(t *testing.T) {
	body := append(grpcTestFrame(0x00, []byte("one")), grpcTestFrame(0x00, []byte("two"))...)
	body = append(body, grpcTestFrame(0x80, []byte("grpc-status: 0\r\ngrpc-message: hello%20world\r\n"))...)

	response, err := (GRPCClient{}).ParseResponse(body, "application/grpc-web+proto", nil)
	if err != nil {
		t.Fatalf("ParseResponse returned error: %v", err)
	}

	if len(response.Messages) != 2 || string(response.Messages[0]) != "one" || string(response.Messages[1]) != "two" {
		t.Fatalf("messages = %#v", response.Messages)
	}
	if response.Trailers["grpc-status"] != "0" || response.Trailers["grpc-message"] != "hello world" {
		t.Fatalf("trailers = %#v", response.Trailers)
	}
	if status := (GRPCClient{}).GetStatus(response.Trailers); !status.OK() || status.Message != "hello world" {
		t.Fatalf("status = %#v", status)
	}
}

func TestGRPCClientParseResponseHandlesTextAndHeaderTrailers(t *testing.T) {
	body := base64.StdEncoding.EncodeToString(grpcTestFrame(0x00, []byte("payload")))
	response, err := (GRPCClient{}).ParseResponse([]byte(body), "application/grpc-web-text", map[string]string{
		"Grpc-Status":  "7",
		"Grpc-Message": "permission%20denied",
	})
	if err != nil {
		t.Fatalf("ParseResponse returned error: %v", err)
	}

	if len(response.Messages) != 1 || string(response.Messages[0]) != "payload" {
		t.Fatalf("messages = %#v", response.Messages)
	}
	if response.Trailers["grpc-status"] != "7" || response.Trailers["grpc-message"] != "permission denied" {
		t.Fatalf("trailers = %#v", response.Trailers)
	}
}

func TestGRPCClientParseResponseAutoDecodesBase64LookingBody(t *testing.T) {
	body := base64.StdEncoding.EncodeToString(grpcTestFrame(0x00, []byte("auto")))
	response, err := (GRPCClient{}).ParseResponse([]byte(body), "", nil)
	if err != nil {
		t.Fatalf("ParseResponse returned error: %v", err)
	}

	if len(response.Messages) != 1 || string(response.Messages[0]) != "auto" {
		t.Fatalf("messages = %#v", response.Messages)
	}
}

func TestGRPCClientParseResponseRejectsCompressedFrame(t *testing.T) {
	_, err := (GRPCClient{}).ParseResponse(grpcTestFrame(0x01, []byte("compressed")), "", nil)
	if err == nil {
		t.Fatal("ParseResponse should reject compressed frames")
	}
}

func grpcTestFrame(flag byte, payload []byte) []byte {
	frame := []byte{flag, byte(len(payload) >> 24), byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	return append(frame, payload...)
}
