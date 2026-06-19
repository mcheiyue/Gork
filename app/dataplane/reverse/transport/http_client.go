package transport

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"golang.org/x/net/proxy"
)

type netHTTPClient struct{}

func (netHTTPClient) Post(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	return doHTTPRequest(ctx, http.MethodPost, request)
}

func (netHTTPClient) Get(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	return doHTTPRequest(ctx, http.MethodGet, request)
}

func (netHTTPClient) Delete(ctx context.Context, request HTTPRequest) (HTTPResponse, error) {
	return doHTTPRequest(ctx, http.MethodDelete, request)
}

func doHTTPRequest(ctx context.Context, method string, request HTTPRequest) (HTTPResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, request.Timeout)
	rawRequest, err := http.NewRequestWithContext(ctx, method, httpRequestURL(request), bytes.NewReader(request.Payload))
	if err != nil {
		cancel()
		return HTTPResponse{}, err
	}
	for key, value := range request.Headers {
		rawRequest.Header.Set(key, value)
	}
	client := httpClientForLease(request.Lease, request.Timeout)
	response, err := client.Do(rawRequest)
	if err != nil {
		cancel()
		return HTTPResponse{}, err
	}
	if request.Stream && response.StatusCode == 200 {
		return HTTPResponse{
			StatusCode: response.StatusCode,
			Headers:    firstHeaderValues(response.Header),
			Stream:     &cancelOnCloseReader{ReadCloser: response.Body, cancel: cancel},
		}, nil
	}
	defer cancel()
	defer response.Body.Close()
	body, err := readHTTPResponseBody(response)
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{StatusCode: response.StatusCode, Body: body, Headers: firstHeaderValues(response.Header)}, nil
}

func readHTTPResponseBody(response *http.Response) ([]byte, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	encoding := response.Header.Get("Content-Encoding")
	switch encoding {
	case "gzip":
		return decodeGzipBody(body)
	case "deflate":
		return decodeDeflateBody(body)
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		return decodeGzipBody(body)
	}
	return body, nil
}

func decodeGzipBody(body []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return body, nil
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return body, nil
	}
	return decoded, nil
}

func decodeDeflateBody(body []byte) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(body))
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return body, nil
	}
	return decoded, nil
}

func httpRequestURL(request HTTPRequest) string {
	if len(request.Params) == 0 {
		return request.URL
	}
	parsed, err := url.Parse(request.URL)
	if err != nil {
		return request.URL
	}
	query := parsed.Query()
	for key, value := range request.Params {
		query.Set(key, fmt.Sprint(value))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// httpClientForLease returns an *http.Client that routes through the SOCKS5 proxy
// specified in the lease. If the lease has no proxy, it returns http.DefaultClient.
func httpClientForLease(lease *controlproxy.ProxyLease, timeout time.Duration) *http.Client {
	if lease == nil || lease.ProxyURL == nil || *lease.ProxyURL == "" {
		return http.DefaultClient
	}
	proxyURL := *lease.ProxyURL
	if strings.HasPrefix(proxyURL, "socks5h://") {
		proxyURL = "socks5://" + proxyURL[len("socks5h://"):]
	} else if strings.HasPrefix(proxyURL, "socks4a://") {
		proxyURL = "socks4://" + proxyURL[len("socks4a://"):]
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return http.DefaultClient
	}
	scheme := strings.ToLower(parsed.Scheme)
	if !strings.HasPrefix(scheme, "socks") {
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy: http.ProxyURL(parsed),
			},
		}
	}
	auth := &proxy.Auth{}
	if parsed.User != nil {
		auth.User = parsed.User.Username()
		auth.Password, _ = parsed.User.Password()
	}
	dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
	if err != nil {
		return http.DefaultClient
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.(proxy.ContextDialer).DialContext(ctx, network, addr)
			},
		},
	}
}
