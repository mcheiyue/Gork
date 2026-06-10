package transport

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	"github.com/jiujiu532/grok2api/app/platform/config"
)

const defaultWebSocketTimeout = 120 * time.Second

var (
	webSocketTimeoutProvider = func(defaultSeconds float64) float64 {
		return config.GlobalConfig.GetFloat("voice.timeout", defaultSeconds)
	}
	webSocketSkipSSLVerifyProvider = func() bool {
		return config.GlobalConfig.GetBool("proxy.egress.skip_ssl_verify", false)
	}
)

type WebSocketProxyKind string

const (
	WebSocketProxyDirect WebSocketProxyKind = "direct"
	WebSocketProxyHTTP   WebSocketProxyKind = "http"
	WebSocketProxySocks  WebSocketProxyKind = "socks"
)

type NormalizedSocksProxy struct {
	URL    string
	Scheme string
	RDNS   *bool
}

type WebSocketConnector struct {
	ProxyKind     WebSocketProxyKind
	HTTPProxy     *string
	SocksProxyURL string
	SocksRDNS     *bool
}

type WebSocketSession interface {
	Close() error
}

type WebSocketEndpoint interface {
	Close() error
	Closed() bool
}

type WebSocketDialer interface {
	Connect(ctx context.Context, request WebSocketConnectRequest) (WebSocketSession, WebSocketEndpoint, error)
}

type WebSocketConnectRequest struct {
	URL           string
	Headers       map[string]string
	Timeout       time.Duration
	WSOptions     map[string]any
	ProxyKind     WebSocketProxyKind
	HTTPProxy     *string
	SocksProxyURL string
	SocksRDNS     *bool
	SkipSSLVerify bool
}

type WebSocketConnectOptions struct {
	Headers   map[string]string
	Timeout   time.Duration
	WSOptions map[string]any
	Lease     *controlproxy.ProxyLease
	OnClose   func(context.Context) error
}

type WebSocketClientOptions struct {
	ProxyOverride string
	Dialer        WebSocketDialer
	SkipSSLVerify bool
}

type WebSocketClient struct {
	proxyOverride string
	dialer        WebSocketDialer
	skipSSLVerify bool
}

type WebSocketConnection struct {
	session  WebSocketSession
	endpoint WebSocketEndpoint
	onClose  func(context.Context) error
}

func NormalizeSocksProxy(proxyURL string) NormalizedSocksProxy {
	scheme := websocketScheme(proxyURL)
	base := scheme
	rdns := (*bool)(nil)
	if scheme == "socks" {
		base = "socks5"
		rdns = boolPointer(true)
	} else if scheme == "socks5h" {
		base = "socks5"
		rdns = boolPointer(true)
	} else if scheme == "socks4a" {
		base = "socks4"
		rdns = boolPointer(true)
	}
	if base != scheme && scheme != "" {
		proxyURL = strings.Replace(proxyURL, scheme+"://", base+"://", 1)
	}
	return NormalizedSocksProxy{URL: proxyURL, Scheme: base, RDNS: rdns}
}

func BuildWebSocketConnector(proxyURL string) WebSocketConnector {
	if proxyURL == "" {
		return WebSocketConnector{ProxyKind: WebSocketProxyDirect}
	}
	scheme := websocketScheme(proxyURL)
	if strings.HasPrefix(scheme, "socks") {
		normalized := NormalizeSocksProxy(proxyURL)
		return WebSocketConnector{
			ProxyKind:     WebSocketProxySocks,
			SocksProxyURL: normalized.URL,
			SocksRDNS:     normalized.RDNS,
		}
	}
	return WebSocketConnector{ProxyKind: WebSocketProxyHTTP, HTTPProxy: &proxyURL}
}

func NewWebSocketClient(options WebSocketClientOptions) *WebSocketClient {
	if options.Dialer == nil {
		options.Dialer = missingWebSocketDialer{}
	}
	return &WebSocketClient{
		proxyOverride: options.ProxyOverride,
		dialer:        options.Dialer,
		skipSSLVerify: options.SkipSSLVerify,
	}
}

func NewWebSocketConnection(session WebSocketSession, endpoint WebSocketEndpoint, onClose func(context.Context) error) *WebSocketConnection {
	return &WebSocketConnection{session: session, endpoint: endpoint, onClose: onClose}
}

func (c *WebSocketClient) Connect(ctx context.Context, rawURL string, options WebSocketConnectOptions) (*WebSocketConnection, error) {
	options = normalizeWebSocketConnectOptions(options)
	proxyURL := c.proxyURL(options.Lease)
	connector := BuildWebSocketConnector(proxyURL)
	request := WebSocketConnectRequest{
		URL:           rawURL,
		Headers:       copyStringMap(options.Headers),
		Timeout:       options.Timeout,
		WSOptions:     copyAnyMap(options.WSOptions),
		ProxyKind:     connector.ProxyKind,
		HTTPProxy:     connector.HTTPProxy,
		SocksProxyURL: connector.SocksProxyURL,
		SocksRDNS:     connector.SocksRDNS,
		SkipSSLVerify: proxyURL != "" && (c.skipSSLVerify || webSocketSkipSSLVerifyProvider()),
	}
	session, endpoint, err := c.dialer.Connect(ctx, request)
	if err != nil {
		if session != nil {
			_ = session.Close()
		}
		return nil, err
	}
	return NewWebSocketConnection(session, endpoint, options.OnClose), nil
}

func normalizeWebSocketConnectOptions(options WebSocketConnectOptions) WebSocketConnectOptions {
	if options.Timeout == 0 {
		options.Timeout = configuredWebSocketTimeout()
	}
	return options
}

func configuredWebSocketTimeout() time.Duration {
	seconds := webSocketTimeoutProvider(defaultWebSocketTimeout.Seconds())
	if seconds <= 0 {
		return defaultWebSocketTimeout
	}
	return time.Duration(seconds * float64(time.Second))
}

func (c *WebSocketClient) proxyURL(lease *controlproxy.ProxyLease) string {
	if c.proxyOverride != "" {
		return c.proxyOverride
	}
	if lease != nil && lease.ProxyURL != nil {
		return *lease.ProxyURL
	}
	return ""
}

func (c *WebSocketConnection) Close(ctx context.Context) error {
	var firstErr error
	if c.endpoint != nil && !c.endpoint.Closed() {
		firstErr = c.endpoint.Close()
	}
	if c.session != nil {
		if err := c.session.Close(); firstErr == nil {
			firstErr = err
		}
	}
	if c.onClose != nil {
		onClose := c.onClose
		c.onClose = nil
		if err := onClose(ctx); firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func websocketScheme(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme)
}

func copyStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := map[string]string{}
	for key, value := range input {
		output[key] = value
	}
	return output
}

func copyAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := map[string]any{}
	for key, value := range input {
		output[key] = value
	}
	return output
}

func boolPointer(value bool) *bool {
	return &value
}

type missingWebSocketDialer struct{}

func (missingWebSocketDialer) Connect(context.Context, WebSocketConnectRequest) (WebSocketSession, WebSocketEndpoint, error) {
	return nil, nil, fmt.Errorf("websocket dialer is not configured")
}
