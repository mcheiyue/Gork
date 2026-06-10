package transport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

func TestNormalizeSocksProxyMatchesPython(t *testing.T) {
	tests := []struct {
		raw        string
		wantURL    string
		wantRDNS   *bool
		wantScheme string
	}{
		{raw: "socks://proxy.test:1080", wantURL: "socks5://proxy.test:1080", wantRDNS: wsBoolPtr(true), wantScheme: "socks5"},
		{raw: "socks5h://proxy.test:1080", wantURL: "socks5://proxy.test:1080", wantRDNS: wsBoolPtr(true), wantScheme: "socks5"},
		{raw: "socks4a://proxy.test:1080", wantURL: "socks4://proxy.test:1080", wantRDNS: wsBoolPtr(true), wantScheme: "socks4"},
		{raw: "socks5://proxy.test:1080", wantURL: "socks5://proxy.test:1080", wantRDNS: nil, wantScheme: "socks5"},
	}
	for _, tt := range tests {
		got := NormalizeSocksProxy(tt.raw)
		if got.URL != tt.wantURL || got.Scheme != tt.wantScheme || !reflect.DeepEqual(got.RDNS, tt.wantRDNS) {
			t.Fatalf("NormalizeSocksProxy(%q) = %#v", tt.raw, got)
		}
	}
}

func TestBuildWebSocketConnectorClassifiesDirectHTTPAndSocks(t *testing.T) {
	direct := BuildWebSocketConnector("")
	if direct.ProxyKind != WebSocketProxyDirect || direct.HTTPProxy != nil {
		t.Fatalf("direct connector = %#v", direct)
	}

	httpProxy := "https://proxy.test:8443"
	httpConnector := BuildWebSocketConnector(httpProxy)
	if httpConnector.ProxyKind != WebSocketProxyHTTP || httpConnector.HTTPProxy == nil || *httpConnector.HTTPProxy != httpProxy {
		t.Fatalf("http connector = %#v", httpConnector)
	}

	socks := BuildWebSocketConnector("socks5h://proxy.test:1080")
	if socks.ProxyKind != WebSocketProxySocks || socks.HTTPProxy != nil || socks.SocksProxyURL != "socks5://proxy.test:1080" ||
		socks.SocksRDNS == nil || *socks.SocksRDNS != true {
		t.Fatalf("socks connector = %#v", socks)
	}
}

func TestWebSocketConnectionCloseClosesEndpointSessionAndOnCloseOnce(t *testing.T) {
	endpoint := &fakeWebSocketEndpoint{}
	session := &fakeWebSocketSession{}
	calls := 0
	conn := NewWebSocketConnection(session, endpoint, func(context.Context) error {
		calls++
		return nil
	})

	if err := conn.Close(context.Background()); err != nil {
		t.Fatalf("close returned error: %v", err)
	}
	if endpoint.closeCalls != 1 || session.closeCalls != 1 || calls != 1 {
		t.Fatalf("close state endpoint=%d session=%d onClose=%d", endpoint.closeCalls, session.closeCalls, calls)
	}
	if err := conn.Close(context.Background()); err != nil {
		t.Fatalf("second close returned error: %v", err)
	}
	if endpoint.closeCalls != 1 || calls != 1 {
		t.Fatalf("second close should not close endpoint/onClose again: endpoint=%d onClose=%d", endpoint.closeCalls, calls)
	}
}

func TestWebSocketClientConnectBuildsRequestAndClosesSessionOnError(t *testing.T) {
	proxyURL := "https://lease-proxy.test:8443"
	lease := controlproxy.NewProxyLease("lease-1")
	lease.ProxyURL = &proxyURL
	endpoint := &fakeWebSocketEndpoint{}
	session := &fakeWebSocketSession{}
	dialer := &fakeWebSocketDialer{session: session, endpoint: endpoint}
	client := NewWebSocketClient(WebSocketClientOptions{Dialer: dialer})

	conn, err := client.Connect(context.Background(), "wss://grok.test/ws", WebSocketConnectOptions{
		Headers:   map[string]string{"A": "B"},
		Timeout:   7 * time.Second,
		WSOptions: map[string]any{"heartbeat": 20},
		Lease:     &lease,
	})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if conn == nil || len(dialer.requests) != 1 {
		t.Fatalf("connect result conn=%#v requests=%#v", conn, dialer.requests)
	}
	request := dialer.requests[0]
	if request.URL != "wss://grok.test/ws" || request.Timeout != 7*time.Second || request.ProxyKind != WebSocketProxyHTTP ||
		request.HTTPProxy == nil || *request.HTTPProxy != proxyURL || !reflect.DeepEqual(request.Headers, map[string]string{"A": "B"}) ||
		!reflect.DeepEqual(request.WSOptions, map[string]any{"heartbeat": 20}) {
		t.Fatalf("connect request = %#v", request)
	}

	overrideDialer := &fakeWebSocketDialer{err: errors.New("dial failed"), session: &fakeWebSocketSession{}}
	overrideClient := NewWebSocketClient(WebSocketClientOptions{ProxyOverride: "socks://override.test:1080", Dialer: overrideDialer, SkipSSLVerify: true})
	_, err = overrideClient.Connect(context.Background(), "wss://grok.test/ws", WebSocketConnectOptions{})
	if err == nil {
		t.Fatalf("Connect should return dial error")
	}
	errorRequest := overrideDialer.requests[0]
	if errorRequest.ProxyKind != WebSocketProxySocks || errorRequest.SocksProxyURL != "socks5://override.test:1080" || !errorRequest.SkipSSLVerify {
		t.Fatalf("override request = %#v", errorRequest)
	}
	if overrideDialer.session.closeCalls != 1 {
		t.Fatalf("dial error should close session, closeCalls=%d", overrideDialer.session.closeCalls)
	}
}

func TestWebSocketClientUsesConfiguredTimeoutAndSkipSSLOnlyWithProxy(t *testing.T) {
	resetWebSocketConfigForTest(t, 12.5, true)

	directDialer := &fakeWebSocketDialer{session: &fakeWebSocketSession{}, endpoint: &fakeWebSocketEndpoint{}}
	directClient := NewWebSocketClient(WebSocketClientOptions{Dialer: directDialer})
	if _, err := directClient.Connect(context.Background(), "wss://grok.test/ws", WebSocketConnectOptions{}); err != nil {
		t.Fatalf("direct Connect returned error: %v", err)
	}
	directRequest := directDialer.requests[0]
	if directRequest.Timeout != 12500*time.Millisecond {
		t.Fatalf("direct timeout = %s, want configured 12.5s", directRequest.Timeout)
	}
	if directRequest.SkipSSLVerify {
		t.Fatalf("direct request should not skip SSL without proxy: %#v", directRequest)
	}

	proxyURL := "https://lease-proxy.test:8443"
	lease := controlproxy.NewProxyLease("lease-2")
	lease.ProxyURL = &proxyURL
	proxyDialer := &fakeWebSocketDialer{session: &fakeWebSocketSession{}, endpoint: &fakeWebSocketEndpoint{}}
	proxyClient := NewWebSocketClient(WebSocketClientOptions{Dialer: proxyDialer})
	if _, err := proxyClient.Connect(context.Background(), "wss://grok.test/ws", WebSocketConnectOptions{Lease: &lease}); err != nil {
		t.Fatalf("proxy Connect returned error: %v", err)
	}
	proxyRequest := proxyDialer.requests[0]
	if proxyRequest.Timeout != 12500*time.Millisecond || !proxyRequest.SkipSSLVerify {
		t.Fatalf("proxy request timeout/skip ssl = %#v", proxyRequest)
	}
}

func TestWebSocketClientExplicitTimeoutOverridesConfigAndInvalidConfigFallsBack(t *testing.T) {
	resetWebSocketConfigForTest(t, 99, false)
	options := normalizeWebSocketConnectOptions(WebSocketConnectOptions{Timeout: 3 * time.Second})
	if options.Timeout != 3*time.Second {
		t.Fatalf("timeout = %s, want explicit 3s", options.Timeout)
	}

	resetWebSocketConfigForTest(t, -1, false)
	options = normalizeWebSocketConnectOptions(WebSocketConnectOptions{})
	if options.Timeout != defaultWebSocketTimeout {
		t.Fatalf("timeout = %s, want %s", options.Timeout, defaultWebSocketTimeout)
	}
}

type fakeWebSocketEndpoint struct {
	closeCalls int
	closed     bool
}

func (e *fakeWebSocketEndpoint) Close() error {
	e.closeCalls++
	e.closed = true
	return nil
}

func (e *fakeWebSocketEndpoint) Closed() bool {
	return e.closed
}

type fakeWebSocketSession struct {
	closeCalls int
}

func (s *fakeWebSocketSession) Close() error {
	s.closeCalls++
	return nil
}

type fakeWebSocketDialer struct {
	requests []WebSocketConnectRequest
	session  *fakeWebSocketSession
	endpoint *fakeWebSocketEndpoint
	err      error
}

func (d *fakeWebSocketDialer) Connect(_ context.Context, request WebSocketConnectRequest) (WebSocketSession, WebSocketEndpoint, error) {
	d.requests = append(d.requests, request)
	if d.err != nil {
		return d.session, nil, d.err
	}
	return d.session, d.endpoint, nil
}

func resetWebSocketConfigForTest(t *testing.T, timeout float64, skipSSLVerify bool) {
	t.Helper()
	previousTimeoutProvider := webSocketTimeoutProvider
	previousSkipProvider := webSocketSkipSSLVerifyProvider
	webSocketTimeoutProvider = func(float64) float64 { return timeout }
	webSocketSkipSSLVerifyProvider = func() bool { return skipSSLVerify }
	t.Cleanup(func() {
		webSocketTimeoutProvider = previousTimeoutProvider
		webSocketSkipSSLVerifyProvider = previousSkipProvider
	})
}

func wsBoolPtr(value bool) *bool {
	return &value
}
