package adapters

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type CurlOption string

const (
	CurlOptProxySSLVerifyPeer CurlOption = "PROXY_SSL_VERIFYPEER"
	CurlOptProxySSLVerifyHost CurlOption = "PROXY_SSL_VERIFYHOST"
)

type SessionKwargs map[string]any

type SessionConfig interface {
	GetBool(key string, defaultValue bool) bool
	GetList(key string, defaultValue []int) []int
}

type globalSessionConfig struct{}

func (globalSessionConfig) GetBool(key string, defaultValue bool) bool {
	return platformconfig.GlobalConfig.GetBool(key, defaultValue)
}

func (globalSessionConfig) GetList(key string, defaultValue []int) []int {
	defaultAny := make([]any, 0, len(defaultValue))
	for _, value := range defaultValue {
		defaultAny = append(defaultAny, value)
	}
	values := platformconfig.GlobalConfig.GetList(key, defaultAny)
	out := make([]int, 0, len(values))
	for _, value := range values {
		if parsed, ok := intFromAny(value); ok {
			out = append(out, parsed)
		}
	}
	return out
}

type BuildSessionOptions struct {
	Lease           *controlproxy.ProxyLease
	BrowserOverride string
	Extra           map[string]any
	Config          SessionConfig
}

func NormalizeProxyURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "socks":
		return "socks5h://" + rawURL[len("socks://"):]
	case "socks5":
		return "socks5h://" + rawURL[len("socks5://"):]
	case "socks4":
		return "socks4a://" + rawURL[len("socks4://"):]
	default:
		return rawURL
	}
}

func BuildSessionKwargs(options BuildSessionOptions) SessionKwargs {
	kwargs := copySessionKwargs(options.Extra)

	if !hasTruthyString(kwargs, "impersonate") {
		browser := options.BrowserOverride
		if browser == "" {
			browser = ResolveProxyProfile(options.Lease).Browser
		}
		if browser != "" {
			kwargs["impersonate"] = browser
		}
	}

	proxyURL := ""
	if options.Lease != nil && options.Lease.ProxyURL != nil && *options.Lease.ProxyURL != "" {
		proxyURL = NormalizeProxyURL(*options.Lease.ProxyURL)
		scheme := proxyScheme(proxyURL)
		if strings.HasPrefix(scheme, "socks") {
			setDefault(kwargs, "proxy", proxyURL)
		} else {
			setDefault(kwargs, "proxies", map[string]string{"http": proxyURL, "https": proxyURL})
		}
	}

	if SkipProxySSL(proxyURL, options.Config) {
		opts := copyCurlOptions(kwargs["curl_options"])
		opts[CurlOptProxySSLVerifyPeer] = 0
		opts[CurlOptProxySSLVerifyHost] = 0
		kwargs["curl_options"] = opts
	}

	return kwargs
}

func SkipProxySSL(proxyURL string, config SessionConfig) bool {
	if proxyURL == "" {
		return false
	}
	if config == nil {
		config = globalSessionConfig{}
	}
	return config.GetBool("proxy.egress.skip_ssl_verify", false)
}

func WrapTransportError(err error) *platform.UpstreamError {
	if upstream, ok := err.(*platform.UpstreamError); ok {
		return upstream
	}
	body := strings.ReplaceAll(err.Error(), "\n", "\\n")
	if len(body) > 400 {
		body = body[:400]
	}
	return platform.NewUpstreamError(fmt.Sprintf("Transport request failed: %v", err), 502, body)
}

type SessionResponse struct {
	StatusCode int
}

type SessionClient interface {
	Request(ctx context.Context, method string, args ...any) (SessionResponse, error)
	Close(ctx context.Context) error
}

type SessionFactory func(SessionKwargs) SessionClient

type ResettableSessionOptions struct {
	Lease           *controlproxy.ProxyLease
	BrowserOverride string
	ResetOnStatus   []int
	SessionKwargs   map[string]any
	Config          SessionConfig
	Factory         SessionFactory
}

type ResettableSession struct {
	kwargs       SessionKwargs
	resetOn      map[int]struct{}
	resetPending bool
	lock         sync.Mutex
	session      SessionClient
	factory      SessionFactory
}

func NewResettableSession(options ResettableSessionOptions) *ResettableSession {
	config := options.Config
	if config == nil {
		config = globalSessionConfig{}
	}
	kwargs := BuildSessionKwargs(BuildSessionOptions{
		Lease:           options.Lease,
		BrowserOverride: options.BrowserOverride,
		Extra:           options.SessionKwargs,
		Config:          config,
	})
	resetCodes := options.ResetOnStatus
	if resetCodes == nil {
		resetCodes = []int{403}
		resetCodes = config.GetList("retry.reset_session_status_codes", resetCodes)
	}
	factory := options.Factory
	if factory == nil {
		factory = func(SessionKwargs) SessionClient { return noopSessionClient{} }
	}
	session := &ResettableSession{
		kwargs:  kwargs,
		resetOn: intSet(resetCodes),
		factory: factory,
	}
	session.session = session.create()
	return session
}

func (s *ResettableSession) create() SessionClient {
	return s.factory(s.kwargs)
}

func (s *ResettableSession) maybeReset(ctx context.Context) {
	if !s.resetPending {
		return
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if !s.resetPending {
		return
	}
	s.resetPending = false
	old := s.session
	s.session = s.create()
	if old != nil {
		_ = old.Close(ctx)
	}
}

func (s *ResettableSession) request(ctx context.Context, method string, args ...any) (SessionResponse, error) {
	s.maybeReset(ctx)
	response, err := s.session.Request(ctx, method, args...)
	if err != nil {
		s.resetPending = true
		return SessionResponse{}, WrapTransportError(err)
	}
	if _, ok := s.resetOn[response.StatusCode]; ok && len(s.resetOn) > 0 {
		s.resetPending = true
	}
	return response, nil
}

func (s *ResettableSession) Get(ctx context.Context, args ...any) (SessionResponse, error) {
	return s.request(ctx, "get", args...)
}

func (s *ResettableSession) Post(ctx context.Context, args ...any) (SessionResponse, error) {
	return s.request(ctx, "post", args...)
}

func (s *ResettableSession) Delete(ctx context.Context, args ...any) (SessionResponse, error) {
	return s.request(ctx, "delete", args...)
}

func (s *ResettableSession) Close(ctx context.Context) error {
	if s.session == nil {
		return nil
	}
	err := s.session.Close(ctx)
	s.session = nil
	return err
}

type noopSessionClient struct{}

func (noopSessionClient) Request(context.Context, string, ...any) (SessionResponse, error) {
	return SessionResponse{}, fmt.Errorf("session client factory is not configured")
}

func (noopSessionClient) Close(context.Context) error {
	return nil
}

func copySessionKwargs(extra map[string]any) SessionKwargs {
	kwargs := SessionKwargs{}
	for key, value := range extra {
		kwargs[key] = value
	}
	return kwargs
}

func copyCurlOptions(value any) map[CurlOption]int {
	copied := map[CurlOption]int{}
	switch opts := value.(type) {
	case map[CurlOption]int:
		for key, value := range opts {
			copied[key] = value
		}
	case map[string]int:
		for key, value := range opts {
			copied[CurlOption(key)] = value
		}
	}
	return copied
}

func setDefault(kwargs SessionKwargs, key string, value any) {
	if _, exists := kwargs[key]; !exists {
		kwargs[key] = value
	}
}

func hasTruthyString(kwargs SessionKwargs, key string) bool {
	value, exists := kwargs[key]
	if !exists {
		return false
	}
	if text, ok := value.(string); ok {
		return text != ""
	}
	return value != nil
}

func proxyScheme(proxyURL string) string {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme)
}

func intSet(values []int) map[int]struct{} {
	set := make(map[int]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}
