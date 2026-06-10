package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"
)

type netHTTPAssetClient struct{}

func (netHTTPAssetClient) Post(ctx context.Context, rawURL string, headers map[string]string, body []byte, timeout time.Duration) (AssetHTTPResponse, error) {
	return doAssetHTTPRequest(ctx, http.MethodPost, rawURL, headers, body, timeout)
}

func (netHTTPAssetClient) Get(ctx context.Context, rawURL string, headers map[string]string, timeout time.Duration) (AssetHTTPResponse, error) {
	return doAssetHTTPRequest(ctx, http.MethodGet, rawURL, headers, nil, timeout)
}

func doAssetHTTPRequest(ctx context.Context, method string, rawURL string, headers map[string]string, body []byte, timeout time.Duration) (AssetHTTPResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return AssetHTTPResponse{}, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return AssetHTTPResponse{}, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return AssetHTTPResponse{}, err
	}
	return AssetHTTPResponse{
		StatusCode: response.StatusCode,
		Body:       responseBody,
		Headers:    firstHeaderValues(response.Header),
	}, nil
}

func firstHeaderValues(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			result[key] = values[0]
		}
	}
	return result
}
