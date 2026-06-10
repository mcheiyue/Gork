package protocol

import (
	"context"
	"fmt"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	reverseruntime "github.com/jiujiu532/grok2api/app/dataplane/reverse/runtime"
)

const (
	AccountsOrigin = "https://accounts.x.ai"
	GrokOrigin     = reverseruntime.Base
	AcceptTOSURL   = reverseruntime.AcceptTOS
	NSFWMgmtURL    = reverseruntime.NSFWMgmt
	SetBirthURL    = reverseruntime.SetBirth
)

type GrpcStatus struct {
	Code    int
	Message string
}

func (s GrpcStatus) OK() bool { return s.Code == 0 }

func (s GrpcStatus) HTTPEquiv() int {
	switch s.Code {
	case 0:
		return 200
	case 4:
		return 504
	case 7:
		return 403
	case 8:
		return 429
	case 14:
		return 503
	case 16:
		return 401
	default:
		return 502
	}
}

type BirthPayloadOptions struct {
	Today   func() time.Time
	RandInt func(min, max int) int
}

type GRPCWebRequest struct {
	URL      string
	Token    string
	Payload  []byte
	Lease    *controlproxy.ProxyLease
	TimeoutS float64
	Origin   string
	Referer  string
	Session  any
}

type GRPCWebResponse struct {
	Trailers map[string]string
}

type JSONAuthRequest struct {
	URL      string
	Token    string
	Payload  []byte
	Lease    *controlproxy.ProxyLease
	TimeoutS float64
	Origin   string
	Referer  string
	Session  any
}

type AuthProxy interface {
	Acquire(ctx context.Context, options ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, result controlproxy.ProxyFeedback) error
}

type GRPCWebPoster interface {
	PostGRPCWeb(ctx context.Context, req GRPCWebRequest) (GRPCWebResponse, error)
}

type JSONAuthPoster interface {
	PostJSON(ctx context.Context, req JSONAuthRequest) (map[string]any, error)
}

type AuthClientOptions struct {
	Proxy        AuthProxy
	GRPC         GRPCWebPoster
	JSON         JSONAuthPoster
	TimeoutS     float64
	BirthOptions BirthPayloadOptions
}

type XAIAuthClient struct {
	proxy        AuthProxy
	grpc         GRPCWebPoster
	json         JSONAuthPoster
	timeoutS     float64
	birthOptions BirthPayloadOptions
}

type authCallOptions struct {
	lease   *controlproxy.ProxyLease
	session any
}

func NewXAIAuthClient(options AuthClientOptions) *XAIAuthClient {
	timeout := options.TimeoutS
	if timeout == 0 {
		timeout = 30.0
	}
	proxy := options.Proxy
	if proxy == nil {
		proxy = noopAuthProxy{}
	}
	grpc := options.GRPC
	if grpc == nil {
		grpc = missingGRPCPoster{}
	}
	jsonPoster := options.JSON
	if jsonPoster == nil {
		jsonPoster = missingJSONPoster{}
	}
	return &XAIAuthClient{
		proxy:        proxy,
		grpc:         grpc,
		json:         jsonPoster,
		timeoutS:     timeout,
		birthOptions: options.BirthOptions,
	}
}

func BuildAcceptTOSPayload() []byte {
	return encodeGRPCPayload([]byte{0x10, 0x01})
}

func BuildNSFWMgmtPayload(enabled bool) []byte {
	name := []byte("always_show_nsfw_content")
	inner := append([]byte{0x0a, byte(len(name))}, name...)
	value := byte(0x00)
	if enabled {
		value = 0x01
	}
	protobuf := []byte{0x0a, 0x02, 0x10, value, 0x12, byte(len(inner))}
	protobuf = append(protobuf, inner...)
	return encodeGRPCPayload(protobuf)
}

func BuildSetBirthPayload(options ...BirthPayloadOptions) map[string]string {
	option := birthPayloadOptions(options...)
	today := option.Today()
	birthYear := today.Year() - option.RandInt(20, 48)
	value := fmt.Sprintf(
		"%04d-%02d-%02dT%02d:%02d:%02d.%03dZ",
		birthYear,
		option.RandInt(1, 12),
		option.RandInt(1, 28),
		option.RandInt(0, 23),
		option.RandInt(0, 59),
		option.RandInt(0, 59),
		option.RandInt(0, 999),
	)
	return map[string]string{"birthDate": value}
}

func (c *XAIAuthClient) AcceptTOS(ctx context.Context, token string) (GrpcStatus, error) {
	return c.grpcCall(ctx, AcceptTOSURL, token, BuildAcceptTOSPayload(), "accept_tos", AccountsOrigin, AccountsOrigin+"/accept-tos", authCallOptions{})
}

func (c *XAIAuthClient) SetNSFW(ctx context.Context, token string, enabled bool) (GrpcStatus, error) {
	label := "disable_nsfw"
	if enabled {
		label = "enable_nsfw"
	}
	return c.grpcCall(ctx, NSFWMgmtURL, token, BuildNSFWMgmtPayload(enabled), label, GrokOrigin, GrokOrigin+"/?_s=data", authCallOptions{})
}

func (c *XAIAuthClient) EnableNSFW(ctx context.Context, token string) (GrpcStatus, error) {
	return c.SetNSFW(ctx, token, true)
}

func (c *XAIAuthClient) DisableNSFW(ctx context.Context, token string) (GrpcStatus, error) {
	return c.SetNSFW(ctx, token, false)
}

func (c *XAIAuthClient) SetBirthDate(ctx context.Context, token string) (map[string]any, error) {
	return c.setBirthDate(ctx, token, authCallOptions{})
}

func (c *XAIAuthClient) NSFWSequence(ctx context.Context, token string) error {
	if _, err := c.AcceptTOS(ctx, token); err != nil {
		return err
	}
	lease, err := c.proxy.Acquire(ctx, controlproxy.AcquireOptions{
		Scope:           controlproxy.ProxyScopeApp,
		Kind:            controlproxy.RequestKindHTTP,
		ClearanceOrigin: GrokOrigin,
	})
	if err != nil {
		return err
	}
	shared := authCallOptions{lease: &lease, session: struct{}{}}
	if _, err := c.setBirthDate(ctx, token, shared); err != nil {
		return c.sequenceFeedbackError(ctx, lease, err)
	}
	if _, err := c.grpcCall(ctx, NSFWMgmtURL, token, BuildNSFWMgmtPayload(true), "enable_nsfw", GrokOrigin, GrokOrigin+"/?_s=data", shared); err != nil {
		return c.sequenceFeedbackError(ctx, lease, err)
	}
	return c.proxy.Feedback(ctx, lease, proxyFeedback(controlproxy.ProxyFeedbackSuccess, 200))
}

func AcceptTOS(ctx context.Context, token string, options ...AuthClientOptions) (GrpcStatus, error) {
	return NewXAIAuthClient(firstAuthOptions(options)).AcceptTOS(ctx, token)
}

func SetNSFW(ctx context.Context, token string, enabled bool, options ...AuthClientOptions) (GrpcStatus, error) {
	return NewXAIAuthClient(firstAuthOptions(options)).SetNSFW(ctx, token, enabled)
}

func EnableNSFW(ctx context.Context, token string, options ...AuthClientOptions) (GrpcStatus, error) {
	return NewXAIAuthClient(firstAuthOptions(options)).EnableNSFW(ctx, token)
}

func DisableNSFW(ctx context.Context, token string, options ...AuthClientOptions) (GrpcStatus, error) {
	return NewXAIAuthClient(firstAuthOptions(options)).DisableNSFW(ctx, token)
}

func SetBirthDate(ctx context.Context, token string, options ...AuthClientOptions) (map[string]any, error) {
	return NewXAIAuthClient(firstAuthOptions(options)).SetBirthDate(ctx, token)
}

func NSFWSequence(ctx context.Context, token string, options ...AuthClientOptions) error {
	return NewXAIAuthClient(firstAuthOptions(options)).NSFWSequence(ctx, token)
}
