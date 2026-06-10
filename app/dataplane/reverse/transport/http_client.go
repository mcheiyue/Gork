package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	response, err := http.DefaultClient.Do(rawRequest)
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
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{StatusCode: response.StatusCode, Body: body, Headers: firstHeaderValues(response.Header)}, nil
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
