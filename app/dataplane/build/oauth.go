package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OAuthClient 对接 auth.x.ai Device OAuth。
type OAuthClient struct {
	http   *http.Client
	config OAuthConfig
}

// NewOAuthClient 创建 OAuth 客户端；httpClient 为空则用默认超时客户端。
func NewOAuthClient(httpClient *http.Client, cfg OAuthConfig) *OAuthClient {
	cfg = cfg.Normalize()
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthClient{http: httpClient, config: cfg}
}

// StartDevice POST device/code，返回用户验证信息。
func (c *OAuthClient) StartDevice(ctx context.Context) (DeviceAuthorization, error) {
	form := url.Values{
		"client_id": {c.config.ClientID},
		"scope":     {c.config.Scope},
	}
	var payload struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		Interval                int    `json:"interval"`
		ExpiresIn               int    `json:"expires_in"`
	}
	if err := c.postForm(ctx, c.config.DeviceURL, form, &payload); err != nil {
		return DeviceAuthorization{}, err
	}
	if payload.DeviceCode == "" || payload.UserCode == "" || payload.VerificationURI == "" {
		return DeviceAuthorization{}, fmt.Errorf("xAI Device OAuth 返回字段不完整")
	}
	if payload.Interval <= 0 {
		payload.Interval = 5
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 1800
	}
	return DeviceAuthorization{
		DeviceCode:              payload.DeviceCode,
		UserCode:                payload.UserCode,
		VerificationURI:         payload.VerificationURI,
		VerificationURIComplete: payload.VerificationURIComplete,
		Interval:                time.Duration(payload.Interval) * time.Second,
		ExpiresIn:               time.Duration(payload.ExpiresIn) * time.Second,
	}, nil
}

// PollDevice 用 device_code 换 token；pending/slow_down 返回哨兵错误。
func (c *OAuthClient) PollDevice(ctx context.Context, deviceCode string) (TokenPayload, error) {
	form := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {c.config.ClientID},
		"device_code": {deviceCode},
	}
	return c.exchange(ctx, form, "")
}

// Refresh 用 refresh_token 换新 access_token。
func (c *OAuthClient) Refresh(ctx context.Context, refreshToken string) (TokenPayload, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.config.ClientID},
		"refresh_token": {refreshToken},
	}
	value, err := c.exchange(ctx, form, refreshToken)
	if errors.Is(err, ErrAuthorizationDenied) {
		return TokenPayload{}, &RefreshError{
			Code:      "refresh_denied",
			Permanent: true,
			Cause:     err,
		}
	}
	return value, err
}

func (c *OAuthClient) exchange(ctx context.Context, form url.Values, fallbackRefresh string) (TokenPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenPayload{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return TokenPayload{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenPayload{}, err
	}
	var value struct {
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		ExpiresIn        int    `json:"expires_in"`
		IDToken          string `json:"id_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return TokenPayload{}, fmt.Errorf("解析 xAI OAuth 响应: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch value.Error {
		case "authorization_pending":
			return TokenPayload{}, ErrAuthorizationPending
		case "slow_down":
			return TokenPayload{}, ErrSlowDown
		case "access_denied", "expired_token":
			return TokenPayload{}, ErrAuthorizationDenied
		default:
			return TokenPayload{}, &RefreshError{
				Status:     resp.StatusCode,
				Code:       firstNonEmpty(value.Error, "oauth_http_"+strconv.Itoa(resp.StatusCode)),
				Permanent:  resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized,
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			}
		}
	}
	if value.AccessToken == "" {
		return TokenPayload{}, &RefreshError{
			Status:    resp.StatusCode,
			Code:      "missing_access_token",
			Permanent: true,
		}
	}
	if value.ExpiresIn <= 0 {
		value.ExpiresIn = 3600
	}
	return TokenPayload{
		AccessToken:  value.AccessToken,
		RefreshToken: firstNonEmpty(value.RefreshToken, fallbackRefresh),
		ExpiresAt:    time.Now().UTC().Add(time.Duration(value.ExpiresIn) * time.Second),
		IDToken:      value.IDToken,
	}, nil
}

func (c *OAuthClient) postForm(ctx context.Context, endpoint string, form url.Values, output any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("xAI OAuth 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, output)
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(time.Now()) {
		return time.Until(parsed)
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
