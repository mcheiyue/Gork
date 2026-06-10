package protocol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

type fakeAuthProxy struct {
	acquireCalls []controlproxy.AcquireOptions
	feedbacks    []controlproxy.ProxyFeedback
	leases       []controlproxy.ProxyLease
}

func (f *fakeAuthProxy) Acquire(_ context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	var option controlproxy.AcquireOptions
	if len(options) > 0 {
		option = options[0]
	}
	f.acquireCalls = append(f.acquireCalls, option)
	if len(f.leases) == 0 {
		return controlproxy.NewProxyLease("lease"), nil
	}
	lease := f.leases[0]
	f.leases = f.leases[1:]
	return lease, nil
}

func (f *fakeAuthProxy) Feedback(_ context.Context, _ controlproxy.ProxyLease, result controlproxy.ProxyFeedback) error {
	f.feedbacks = append(f.feedbacks, result)
	return nil
}

type fakeGRPCPoster struct {
	requests []GRPCWebRequest
	trailers []map[string]string
	errs     []error
}

func (f *fakeGRPCPoster) PostGRPCWeb(_ context.Context, req GRPCWebRequest) (GRPCWebResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return GRPCWebResponse{}, err
		}
	}
	trailers := map[string]string{"grpc-status": "0"}
	if len(f.trailers) > 0 {
		trailers = f.trailers[0]
		f.trailers = f.trailers[1:]
	}
	return GRPCWebResponse{Trailers: trailers}, nil
}

type fakeJSONPoster struct {
	requests []JSONAuthRequest
	results  []map[string]any
	errs     []error
}

func (f *fakeJSONPoster) PostJSON(_ context.Context, req JSONAuthRequest) (map[string]any, error) {
	f.requests = append(f.requests, req)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(f.results) > 0 {
		result := f.results[0]
		f.results = f.results[1:]
		return result, nil
	}
	return map[string]any{"ok": true}, nil
}

func TestXAIAuthPayloadBuildersMatchPythonFrames(t *testing.T) {
	acceptHex := hex.EncodeToString(BuildAcceptTOSPayload())
	if acceptHex != "00000000021001" {
		t.Fatalf("accept payload hex = %s", acceptHex)
	}

	enabledHex := hex.EncodeToString(BuildNSFWMgmtPayload(true))
	disabledHex := hex.EncodeToString(BuildNSFWMgmtPayload(false))
	if enabledHex != "00000000200a021001121a0a18616c776179735f73686f775f6e7366775f636f6e74656e74" {
		t.Fatalf("enabled nsfw payload hex = %s", enabledHex)
	}
	if disabledHex != "00000000200a021000121a0a18616c776179735f73686f775f6e7366775f636f6e74656e74" {
		t.Fatalf("disabled nsfw payload hex = %s", disabledHex)
	}
}

func TestBuildSetBirthPayloadMatchesPythonFormatWithDeterministicRandom(t *testing.T) {
	values := []int{20, 1, 2, 3, 4, 5, 6}
	payload := BuildSetBirthPayload(BirthPayloadOptions{
		Today: func() time.Time { return time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC) },
		RandInt: func(_, _ int) int {
			value := values[0]
			values = values[1:]
			return value
		},
	})

	if payload["birthDate"] != "2006-01-02T03:04:05.006Z" {
		t.Fatalf("birth payload = %#v", payload)
	}
}

func TestAcceptTOSUsesAccountsOriginAndSuccessFeedback(t *testing.T) {
	proxy := &fakeAuthProxy{}
	grpc := &fakeGRPCPoster{}
	client := NewXAIAuthClient(AuthClientOptions{Proxy: proxy, GRPC: grpc, TimeoutS: 12})

	status, err := client.AcceptTOS(context.Background(), "token")
	if err != nil {
		t.Fatalf("AcceptTOS() error = %v", err)
	}
	if !status.OK() || len(grpc.requests) != 1 {
		t.Fatalf("status=%#v requests=%d", status, len(grpc.requests))
	}
	req := grpc.requests[0]
	if req.URL != AcceptTOSURL || req.Origin != AccountsOrigin || req.Referer != AccountsOrigin+"/accept-tos" {
		t.Fatalf("request routing = %#v", req)
	}
	if req.Token != "token" || req.TimeoutS != 12 || !bytes.Equal(req.Payload, BuildAcceptTOSPayload()) {
		t.Fatalf("request payload/token/timeout = %#v", req)
	}
	if len(proxy.acquireCalls) != 1 || proxy.acquireCalls[0].ClearanceOrigin != AccountsOrigin {
		t.Fatalf("acquire calls = %#v", proxy.acquireCalls)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackSuccess || proxy.feedbacks[0].StatusCode == nil || *proxy.feedbacks[0].StatusCode != 200 {
		t.Fatalf("feedbacks = %#v", proxy.feedbacks)
	}
}

func TestSetNSFWReportsGrpcErrorAsUpstream5xxFeedback(t *testing.T) {
	proxy := &fakeAuthProxy{}
	grpc := &fakeGRPCPoster{trailers: []map[string]string{{"grpc-status": "7", "grpc-message": "denied"}}}
	client := NewXAIAuthClient(AuthClientOptions{Proxy: proxy, GRPC: grpc})

	_, err := client.SetNSFW(context.Background(), "token", false)
	if err == nil {
		t.Fatalf("SetNSFW() error = nil, want upstream error")
	}
	var upstream *platform.UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != 403 {
		t.Fatalf("error = %#v, want upstream status 403", err)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackUpstream5xx || proxy.feedbacks[0].StatusCode == nil || *proxy.feedbacks[0].StatusCode != 403 {
		t.Fatalf("feedbacks = %#v", proxy.feedbacks)
	}
	if !bytes.Equal(grpc.requests[0].Payload, BuildNSFWMgmtPayload(false)) {
		t.Fatalf("payload = %x", grpc.requests[0].Payload)
	}
}

func TestGRPCSharedCallSkipsAcquireAndFeedbackAndMissingStatusSucceeds(t *testing.T) {
	proxy := &fakeAuthProxy{}
	grpc := &fakeGRPCPoster{trailers: []map[string]string{{}}}
	client := NewXAIAuthClient(AuthClientOptions{Proxy: proxy, GRPC: grpc})
	lease := controlproxy.NewProxyLease("shared")
	session := "session"

	status, err := client.grpcCall(
		context.Background(),
		NSFWMgmtURL,
		"token",
		BuildNSFWMgmtPayload(true),
		"enable_nsfw",
		GrokOrigin,
		GrokOrigin+"/?_s=data",
		authCallOptions{lease: &lease, session: session},
	)
	if err != nil {
		t.Fatalf("shared grpcCall() error = %v", err)
	}
	if status.Code != -1 {
		t.Fatalf("status = %#v, want missing grpc-status code -1", status)
	}
	if len(proxy.acquireCalls) != 0 || len(proxy.feedbacks) != 0 {
		t.Fatalf("shared call acquired or fed back proxy: acquire=%#v feedback=%#v", proxy.acquireCalls, proxy.feedbacks)
	}
	if len(grpc.requests) != 1 || grpc.requests[0].Lease == nil || grpc.requests[0].Lease.LeaseID != "shared" || grpc.requests[0].Session != session {
		t.Fatalf("shared request = %#v", grpc.requests)
	}
}

func TestGRPCUpstreamAndTransportErrorsMatchPythonFeedback(t *testing.T) {
	t.Run("upstream default status", func(t *testing.T) {
		proxy := &fakeAuthProxy{}
		grpc := &fakeGRPCPoster{errs: []error{&platform.UpstreamError{
			AppError: &platform.AppError{Message: "empty status", Kind: platform.ErrorKindUpstream, Code: "upstream_error", Status: 0},
		}}}
		client := NewXAIAuthClient(AuthClientOptions{Proxy: proxy, GRPC: grpc})

		_, err := client.SetNSFW(context.Background(), "token", true)
		var upstream *platform.UpstreamError
		if !errors.As(err, &upstream) || upstream.Status != 0 {
			t.Fatalf("error = %#v, want original upstream status 0", err)
		}
		if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackForbidden || proxy.feedbacks[0].StatusCode == nil || *proxy.feedbacks[0].StatusCode != 502 {
			t.Fatalf("feedbacks = %#v", proxy.feedbacks)
		}
	})

	t.Run("transport wrapped with label", func(t *testing.T) {
		proxy := &fakeAuthProxy{}
		grpc := &fakeGRPCPoster{errs: []error{errors.New("dial failed")}}
		client := NewXAIAuthClient(AuthClientOptions{Proxy: proxy, GRPC: grpc})

		_, err := client.SetNSFW(context.Background(), "token", false)
		var upstream *platform.UpstreamError
		if !errors.As(err, &upstream) || upstream.Message != "disable_nsfw: transport error: dial failed" {
			t.Fatalf("error = %#v, want labeled upstream transport error", err)
		}
		if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackTransportError {
			t.Fatalf("feedbacks = %#v", proxy.feedbacks)
		}
	})
}

func TestEnableDisableNSFWWrappersCallSetNSFW(t *testing.T) {
	proxy := &fakeAuthProxy{}
	grpc := &fakeGRPCPoster{}
	client := NewXAIAuthClient(AuthClientOptions{Proxy: proxy, GRPC: grpc})

	if _, err := client.EnableNSFW(context.Background(), "token"); err != nil {
		t.Fatalf("EnableNSFW() error = %v", err)
	}
	if _, err := client.DisableNSFW(context.Background(), "token"); err != nil {
		t.Fatalf("DisableNSFW() error = %v", err)
	}
	if len(grpc.requests) != 2 {
		t.Fatalf("requests = %#v, want enable and disable", grpc.requests)
	}
	if !bytes.Equal(grpc.requests[0].Payload, BuildNSFWMgmtPayload(true)) {
		t.Fatalf("enable payload = %x", grpc.requests[0].Payload)
	}
	if !bytes.Equal(grpc.requests[1].Payload, BuildNSFWMgmtPayload(false)) {
		t.Fatalf("disable payload = %x", grpc.requests[1].Payload)
	}
}

func TestSetBirthDatePostsJSONAndSuccessFeedback(t *testing.T) {
	proxy := &fakeAuthProxy{}
	jsonPoster := &fakeJSONPoster{results: []map[string]any{{"done": true}}}
	client := NewXAIAuthClient(AuthClientOptions{
		Proxy:    proxy,
		JSON:     jsonPoster,
		TimeoutS: 9,
		BirthOptions: BirthPayloadOptions{
			Today: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
			RandInt: func(min, _ int) int {
				return min
			},
		},
	})

	result, err := client.SetBirthDate(context.Background(), "token")
	if err != nil {
		t.Fatalf("SetBirthDate() error = %v", err)
	}
	if result["done"] != true || len(jsonPoster.requests) != 1 {
		t.Fatalf("result=%#v requests=%d", result, len(jsonPoster.requests))
	}
	req := jsonPoster.requests[0]
	if req.URL != SetBirthURL || req.Origin != GrokOrigin || req.Referer != GrokOrigin+"/?_s=data" || req.TimeoutS != 9 {
		t.Fatalf("json request = %#v", req)
	}
	var payload map[string]string
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		t.Fatalf("payload json error = %v", err)
	}
	if payload["birthDate"] != "2006-01-01T00:00:00.000Z" {
		t.Fatalf("payload = %#v", payload)
	}
	if len(proxy.feedbacks) != 1 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackSuccess {
		t.Fatalf("feedbacks = %#v", proxy.feedbacks)
	}
}

func TestNSFWSequenceMatchesPythonCallOrderAndSharedLease(t *testing.T) {
	accountsLease := controlproxy.NewProxyLease("accounts")
	grokLease := controlproxy.NewProxyLease("grok")
	proxy := &fakeAuthProxy{leases: []controlproxy.ProxyLease{accountsLease, grokLease}}
	grpc := &fakeGRPCPoster{}
	jsonPoster := &fakeJSONPoster{}
	client := NewXAIAuthClient(AuthClientOptions{
		Proxy: proxy,
		GRPC:  grpc,
		JSON:  jsonPoster,
		BirthOptions: BirthPayloadOptions{
			Today: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
			RandInt: func(min, _ int) int {
				return min
			},
		},
	})

	if err := client.NSFWSequence(context.Background(), "token"); err != nil {
		t.Fatalf("NSFWSequence() error = %v", err)
	}
	if len(proxy.acquireCalls) != 2 || proxy.acquireCalls[0].ClearanceOrigin != AccountsOrigin || proxy.acquireCalls[1].ClearanceOrigin != GrokOrigin {
		t.Fatalf("acquire calls = %#v", proxy.acquireCalls)
	}
	if len(grpc.requests) != 2 || grpc.requests[0].URL != AcceptTOSURL || grpc.requests[1].URL != NSFWMgmtURL {
		t.Fatalf("grpc requests = %#v", grpc.requests)
	}
	if len(jsonPoster.requests) != 1 || jsonPoster.requests[0].Lease == nil || jsonPoster.requests[0].Lease.LeaseID != "grok" {
		t.Fatalf("json shared request = %#v", jsonPoster.requests)
	}
	if grpc.requests[1].Lease == nil || grpc.requests[1].Lease.LeaseID != "grok" {
		t.Fatalf("grpc shared lease = %#v", grpc.requests[1].Lease)
	}
	if len(proxy.feedbacks) != 2 || proxy.feedbacks[0].Kind != controlproxy.ProxyFeedbackSuccess || proxy.feedbacks[1].Kind != controlproxy.ProxyFeedbackSuccess {
		t.Fatalf("feedbacks = %#v", proxy.feedbacks)
	}
}
