package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	proxyadapters "github.com/jiujiu532/grok2api/app/dataplane/proxy/adapters"
	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	platform "github.com/jiujiu532/grok2api/app/platform"
)

type GRPCWebOptions struct {
	Client GRPCWebHTTPClient
}

type GRPCWebHTTPClient interface {
	Post(ctx context.Context, request GRPCWebHTTPRequest) (GRPCWebHTTPResponse, error)
}

type GRPCWebHTTPRequest struct {
	URL      string
	Token    string
	Payload  []byte
	TimeoutS float64
	Headers  map[string]string
}

type GRPCWebHTTPResponse struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

type GRPCWebTransportResponse struct {
	Messages [][]byte
	Trailers map[string]string
}

type GRPCWebTransport struct {
	Options GRPCWebOptions
}

func (t GRPCWebTransport) PostGRPCWeb(ctx context.Context, request protocol.GRPCWebRequest) (protocol.GRPCWebResponse, error) {
	response, err := PostGRPCWeb(ctx, request, t.Options)
	if err != nil {
		return protocol.GRPCWebResponse{}, err
	}
	return protocol.GRPCWebResponse{Trailers: response.Trailers}, nil
}

func EncodeGRPCWebPayload(data []byte) []byte {
	frame := make([]byte, 5+len(data))
	frame[0] = 0x00
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(data)))
	copy(frame[5:], data)
	return frame
}

func ParseGRPCWebResponse(body []byte, contentType string, headers map[string]string) (GRPCWebTransportResponse, error) {
	decoded, err := maybeDecodeGRPCWebBase64(body, contentType)
	if err != nil {
		return GRPCWebTransportResponse{}, err
	}
	messages := [][]byte{}
	trailers := map[string]string{}
	for index := 0; index < len(decoded); {
		if len(decoded)-index < 5 {
			break
		}
		flag := decoded[index]
		length := int(binary.BigEndian.Uint32(decoded[index+1 : index+5]))
		index += 5
		if len(decoded)-index < length {
			break
		}
		payload := decoded[index : index+length]
		index += length
		if flag&0x80 != 0 {
			mergeTrailers(trailers, parseGRPCWebTrailers(payload))
			continue
		}
		if flag&0x01 != 0 {
			return GRPCWebTransportResponse{}, errors.New("grpc-web compressed frame is not supported")
		}
		messages = append(messages, append([]byte(nil), payload...))
	}
	mergeHeaderTrailers(trailers, headers)
	return GRPCWebTransportResponse{Messages: messages, Trailers: trailers}, nil
}

func PostGRPCWeb(ctx context.Context, request protocol.GRPCWebRequest, options ...GRPCWebOptions) (GRPCWebTransportResponse, error) {
	option := grpcWebOptions(options...)
	headers := grpcWebHeaders(request)
	timeoutS := request.TimeoutS
	if timeoutS <= 0 {
		timeoutS = 30
	}
	response, err := option.Client.Post(ctx, GRPCWebHTTPRequest{
		URL:      request.URL,
		Token:    request.Token,
		Payload:  request.Payload,
		TimeoutS: timeoutS,
		Headers:  headers,
	})
	if err != nil {
		return GRPCWebTransportResponse{}, grpcWebTransportError(err)
	}
	if response.StatusCode != 200 {
		bodyText := truncateString(string(response.Body), 300)
		return GRPCWebTransportResponse{}, platform.NewUpstreamError(fmt.Sprintf("Upstream returned %d", response.StatusCode), response.StatusCode, bodyText)
	}
	return ParseGRPCWebResponse(response.Body, grpcWebHeaderValue(response.Headers, "content-type"), response.Headers)
}

func grpcWebOptions(options ...GRPCWebOptions) GRPCWebOptions {
	option := GRPCWebOptions{Client: netHTTPGRPCWebClient{}}
	if len(options) > 0 {
		option = options[0]
	}
	if option.Client == nil {
		option.Client = netHTTPGRPCWebClient{}
	}
	return option
}

func grpcWebHeaders(request protocol.GRPCWebRequest) map[string]string {
	contentType := "application/grpc-web+proto"
	headers := proxyadapters.BuildHTTPHeaders(request.Token, proxyadapters.HTTPHeaderOptions{
		ContentType: &contentType,
		Origin:      request.Origin,
		Referer:     request.Referer,
		Lease:       request.Lease,
	})
	headers["Content-Type"] = "application/grpc-web+proto"
	headers["Accept"] = "*/*"
	headers["x-grpc-web"] = "1"
	headers["x-user-agent"] = "connect-es/2.1.1"
	headers["Cache-Control"] = "no-cache"
	headers["Pragma"] = "no-cache"
	headers["Sec-Fetch-Dest"] = "empty"
	return headers
}

func maybeDecodeGRPCWebBase64(body []byte, contentType string) ([]byte, error) {
	compact := bytes.Join(bytes.Fields(body), nil)
	if strings.Contains(strings.ToLower(contentType), "grpc-web-text") {
		decoded, err := base64.StdEncoding.DecodeString(string(compact))
		if err != nil {
			return nil, err
		}
		return decoded, nil
	}
	if looksLikeBase64(body) {
		if decoded, err := base64.StdEncoding.DecodeString(string(compact)); err == nil {
			return decoded, nil
		}
	}
	return body, nil
}

func looksLikeBase64(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	limit := len(body)
	if limit > 2048 {
		limit = 2048
	}
	for _, b := range body[:limit] {
		if b == '\r' || b == '\n' || b == '\t' || b == ' ' {
			continue
		}
		if ('A' <= b && b <= 'Z') || ('a' <= b && b <= 'z') || ('0' <= b && b <= '9') || b == '+' || b == '/' || b == '=' {
			continue
		}
		return false
	}
	return true
}

func parseGRPCWebTrailers(payload []byte) map[string]string {
	result := map[string]string{}
	lines := strings.FieldsFunc(string(payload), func(r rune) bool { return r == '\r' || r == '\n' })
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "grpc-message" {
			value, _ = url.QueryUnescape(value)
		}
		result[key] = value
	}
	return result
}

func mergeTrailers(target map[string]string, source map[string]string) {
	for key, value := range source {
		target[key] = value
	}
}

func mergeHeaderTrailers(trailers map[string]string, headers map[string]string) {
	lower := map[string]string{}
	for key, value := range headers {
		lower[strings.ToLower(key)] = strings.TrimSpace(value)
	}
	for _, key := range []string{"grpc-status", "grpc-message"} {
		if _, exists := trailers[key]; exists {
			continue
		}
		if value, ok := lower[key]; ok {
			if key == "grpc-message" {
				value, _ = url.QueryUnescape(value)
			}
			trailers[key] = value
		}
	}
}

func grpcWebHeaderValue(headers map[string]string, want string) string {
	for key, value := range headers {
		if strings.EqualFold(key, want) {
			return value
		}
	}
	return ""
}

func grpcWebTransportError(err error) error {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		return err
	}
	return platform.NewUpstreamError(fmt.Sprintf("grpc-web post transport error: %v", err), 502, err.Error())
}

type netHTTPGRPCWebClient struct{}

func (netHTTPGRPCWebClient) Post(ctx context.Context, request GRPCWebHTTPRequest) (GRPCWebHTTPResponse, error) {
	timeout := time.Duration(request.TimeoutS * float64(time.Second))
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rawRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, request.URL, bytes.NewReader(request.Payload))
	if err != nil {
		return GRPCWebHTTPResponse{}, err
	}
	for key, value := range request.Headers {
		rawRequest.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(rawRequest)
	if err != nil {
		return GRPCWebHTTPResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return GRPCWebHTTPResponse{}, err
	}
	return GRPCWebHTTPResponse{StatusCode: response.StatusCode, Body: body, Headers: firstHeaderValues(response.Header)}, nil
}
