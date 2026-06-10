package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jiujiu532/grok2api/app/control/proxy"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type FlareSolverrConfig interface {
	proxy.StringConfig
	GetInt(key string, defaultValue int) int
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type FlareSolverrClearanceProvider struct {
	Config FlareSolverrConfig
	Client HTTPDoer
}

type globalFlareSolverrConfig struct{}

func (globalFlareSolverrConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func (globalFlareSolverrConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

type flareSolverrCookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain"`
}

type flareSolverrSolveResult struct {
	Cookies       string
	UserAgent     string
	ClearanceHost string
}

func (p FlareSolverrClearanceProvider) RefreshBundle(ctx context.Context, affinityKey, proxyURL string, targetURL ...string) (proxy.ClearanceBundle, bool, error) {
	cfg := p.Config
	if cfg == nil {
		cfg = globalFlareSolverrConfig{}
	}
	modeValue := cfg.GetString("proxy.clearance.mode", "none")

	mode, err := proxy.ParseClearanceMode(modeValue)
	if err != nil {
		return proxy.ClearanceBundle{}, false, err
	}
	if mode != proxy.ClearanceModeFlareSolverr {
		return proxy.ClearanceBundle{}, false, nil
	}

	fsURL := ""
	timeoutSec := 60
	fsURL = cfg.GetString("proxy.clearance.flaresolverr_url", "")
	timeoutSec = cfg.GetInt("proxy.clearance.timeout_sec", 60)
	if fsURL == "" {
		return proxy.ClearanceBundle{}, false, nil
	}

	target := "https://grok.com"
	if len(targetURL) > 0 {
		target = targetURL[0]
	}
	result, ok, err := p.solve(ctx, fsURL, proxyURL, timeoutSec, target)
	if err != nil || !ok {
		return proxy.ClearanceBundle{}, false, err
	}

	host := result.ClearanceHost
	if host == "" {
		host = "grok.com"
	}
	bundle := proxy.NewClearanceBundle(fmt.Sprintf("flaresolverr:%s@%s", affinityKey, host))
	bundle.CFCookies = result.Cookies
	bundle.UserAgent = result.UserAgent
	bundle.AffinityKey = affinityKey
	bundle.ClearanceHost = host
	return bundle, true, nil
}

func (p FlareSolverrClearanceProvider) solve(ctx context.Context, fsURL, proxyURL string, timeoutSec int, targetURL string) (flareSolverrSolveResult, bool, error) {
	target := strings.TrimSpace(targetURL)
	if target == "" {
		target = "https://grok.com"
	}
	payload := map[string]any{
		"cmd":        "request.get",
		"url":        target,
		"maxTimeout": timeoutSec * 1000,
	}
	if proxyURL != "" {
		payload["proxy"] = map[string]string{"url": proxyURL}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return flareSolverrSolveResult{}, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec+30)*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(fsURL, "/") + "/v1"
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return flareSolverrSolveResult{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return flareSolverrSolveResult{}, false, nil
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return flareSolverrSolveResult{}, false, nil
	}
	if resp.StatusCode >= 400 {
		return flareSolverrSolveResult{}, false, nil
	}

	var result struct {
		Status   string `json:"status"`
		Message  string `json:"message"`
		Solution struct {
			Cookies   []flareSolverrCookie `json:"cookies"`
			UserAgent string               `json:"userAgent"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return flareSolverrSolveResult{}, false, nil
	}
	if result.Status != "ok" {
		return flareSolverrSolveResult{}, false, nil
	}
	if len(result.Solution.Cookies) == 0 {
		return flareSolverrSolveResult{}, false, nil
	}

	host := clearanceHost(target)
	filtered := filterCookiesForHost(result.Solution.Cookies, host)
	chosen := filtered
	if len(chosen) == 0 {
		chosen = result.Solution.Cookies
	}
	if host == "" {
		host = "grok.com"
	}
	return flareSolverrSolveResult{
		Cookies:       extractAllCookies(chosen),
		UserAgent:     result.Solution.UserAgent,
		ClearanceHost: host,
	}, true, nil
}

func clearanceHost(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func filterCookiesForHost(cookies []flareSolverrCookie, host string) []flareSolverrCookie {
	filtered := make([]flareSolverrCookie, 0, len(cookies))
	for _, cookie := range cookies {
		domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
		if host == "" || cookie.Domain == "" || strings.HasSuffix(host, domain) {
			filtered = append(filtered, cookie)
		}
	}
	return filtered
}

func extractAllCookies(cookies []flareSolverrCookie) string {
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		parts = append(parts, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	}
	return strings.Join(parts, "; ")
}
