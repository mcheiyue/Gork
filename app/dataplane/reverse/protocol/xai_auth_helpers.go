package protocol

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

func (c *XAIAuthClient) grpcCall(ctx context.Context, url, token string, payload []byte, label, origin, referer string, options authCallOptions) (GrpcStatus, error) {
	lease, shared, err := c.leaseForCall(ctx, origin, options)
	if err != nil {
		return GrpcStatus{}, err
	}
	req := GRPCWebRequest{URL: url, Token: token, Payload: payload, Lease: lease, TimeoutS: c.timeoutS, Origin: origin, Referer: referer, Session: options.session}
	resp, err := c.grpc.PostGRPCWeb(ctx, req)
	if err != nil {
		return GrpcStatus{}, c.feedbackCallError(ctx, shared, lease, label, err)
	}
	status := grpcStatusFromTrailers(resp.Trailers)
	if status.OK() || status.Code == -1 {
		if !shared {
			return status, c.proxy.Feedback(ctx, *lease, proxyFeedback(controlproxy.ProxyFeedbackSuccess, 200))
		}
		return status, nil
	}
	if !shared {
		if err := c.proxy.Feedback(ctx, *lease, proxyFeedback(controlproxy.ProxyFeedbackUpstream5xx, status.HTTPEquiv())); err != nil {
			return status, err
		}
	}
	return status, platform.NewUpstreamError(fmt.Sprintf("%s: gRPC error code=%d message=%q", label, status.Code, status.Message), status.HTTPEquiv(), "")
}

func (c *XAIAuthClient) setBirthDate(ctx context.Context, token string, options authCallOptions) (map[string]any, error) {
	lease, shared, err := c.leaseForCall(ctx, GrokOrigin, options)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(BuildSetBirthPayload(c.birthOptions))
	if err != nil {
		return nil, err
	}
	req := JSONAuthRequest{URL: SetBirthURL, Token: token, Payload: payload, Lease: lease, TimeoutS: c.timeoutS, Origin: GrokOrigin, Referer: GrokOrigin + "/?_s=data", Session: options.session}
	result, err := c.json.PostJSON(ctx, req)
	if err != nil {
		return nil, c.feedbackCallError(ctx, shared, lease, "set_birth_date", err)
	}
	if !shared {
		if err := c.proxy.Feedback(ctx, *lease, proxyFeedback(controlproxy.ProxyFeedbackSuccess, 200)); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (c *XAIAuthClient) leaseForCall(ctx context.Context, origin string, options authCallOptions) (*controlproxy.ProxyLease, bool, error) {
	shared := options.lease != nil && options.session != nil
	if shared {
		return options.lease, true, nil
	}
	lease, err := c.proxy.Acquire(ctx, controlproxy.AcquireOptions{Scope: controlproxy.ProxyScopeApp, Kind: controlproxy.RequestKindHTTP, ClearanceOrigin: origin})
	if err != nil {
		return nil, false, err
	}
	return &lease, false, nil
}

func (c *XAIAuthClient) feedbackCallError(ctx context.Context, shared bool, lease *controlproxy.ProxyLease, label string, err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		rawStatus := upstream.Status
		status := statusOrDefault(rawStatus)
		if !shared {
			kind := controlproxy.ProxyFeedbackForbidden
			if rawStatus >= 500 {
				kind = controlproxy.ProxyFeedbackUpstream5xx
			}
			if feedbackErr := c.proxy.Feedback(ctx, *lease, proxyFeedback(kind, status)); feedbackErr != nil {
				return feedbackErr
			}
		}
		return err
	}
	if !shared {
		if feedbackErr := c.proxy.Feedback(ctx, *lease, controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)); feedbackErr != nil {
			return feedbackErr
		}
	}
	return platform.NewUpstreamError(fmt.Sprintf("%s: transport error: %v", label, err), 0, "")
}

func (c *XAIAuthClient) sequenceFeedbackError(ctx context.Context, lease controlproxy.ProxyLease, err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		rawStatus := upstream.Status
		status := statusOrDefault(rawStatus)
		kind := controlproxy.ProxyFeedbackForbidden
		if rawStatus >= 500 {
			kind = controlproxy.ProxyFeedbackUpstream5xx
		}
		if feedbackErr := c.proxy.Feedback(ctx, lease, proxyFeedback(kind, status)); feedbackErr != nil {
			return feedbackErr
		}
		return err
	}
	if feedbackErr := c.proxy.Feedback(ctx, lease, controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackTransportError)); feedbackErr != nil {
		return feedbackErr
	}
	return platform.NewUpstreamError(fmt.Sprintf("nsfw_sequence: transport error: %v", err), 0, "")
}

func encodeGRPCPayload(data []byte) []byte {
	out := make([]byte, 5+len(data))
	binary.BigEndian.PutUint32(out[1:5], uint32(len(data)))
	copy(out[5:], data)
	return out
}

func grpcStatusFromTrailers(trailers map[string]string) GrpcStatus {
	raw := strings.TrimSpace(trailers["grpc-status"])
	message := strings.TrimSpace(trailers["grpc-message"])
	code, err := strconv.Atoi(raw)
	if err != nil {
		code = -1
	}
	return GrpcStatus{Code: code, Message: message}
}

func proxyFeedback(kind controlproxy.ProxyFeedbackKind, status int) controlproxy.ProxyFeedback {
	feedback := controlproxy.NewProxyFeedback(kind)
	feedback.StatusCode = &status
	return feedback
}

func statusOrDefault(status int) int {
	if status == 0 {
		return 502
	}
	return status
}

func birthPayloadOptions(options ...BirthPayloadOptions) BirthPayloadOptions {
	option := BirthPayloadOptions{Today: time.Now, RandInt: randomIntInclusive}
	if len(options) > 0 {
		option = options[0]
		if option.Today == nil {
			option.Today = time.Now
		}
		if option.RandInt == nil {
			option.RandInt = randomIntInclusive
		}
	}
	return option
}

func randomIntInclusive(min, max int) int {
	return min + rand.Intn(max-min+1)
}

func firstAuthOptions(options []AuthClientOptions) AuthClientOptions {
	if len(options) == 0 {
		return AuthClientOptions{}
	}
	return options[0]
}

type noopAuthProxy struct{}

func (noopAuthProxy) Acquire(context.Context, ...controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return controlproxy.NewProxyLease(""), nil
}

func (noopAuthProxy) Feedback(context.Context, controlproxy.ProxyLease, controlproxy.ProxyFeedback) error {
	return nil
}

type missingGRPCPoster struct{}

func (missingGRPCPoster) PostGRPCWeb(context.Context, GRPCWebRequest) (GRPCWebResponse, error) {
	return GRPCWebResponse{}, errors.New("grpc transport is not configured")
}

type missingJSONPoster struct{}

func (missingJSONPoster) PostJSON(context.Context, JSONAuthRequest) (map[string]any, error) {
	return nil, errors.New("json transport is not configured")
}
