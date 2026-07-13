package products

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
	"github.com/dslzl/gork/app/dataplane/reverse/protocol"
	reverseruntime "github.com/dslzl/gork/app/dataplane/reverse/runtime"
	"github.com/dslzl/gork/app/dataplane/reverse/transport"
	"github.com/dslzl/gork/app/platform"
	"github.com/dslzl/gork/app/platform/logging"
)

var consoleStreamPosterFactory = func() protocol.ConsoleStreamPoster {
	return consoleHTTPPoster{}
}

var getProxyDirectory = func(ctx context.Context) (*controlproxy.ProxyDirectory, error) {
	return controlproxy.GetProxyDirectory(ctx, proxydataplane.ProductionDirectoryOptions())
}

func StreamConsoleChat(ctx context.Context, token string, payload map[string]any, timeoutS float64) ([]protocol.ConsoleStreamEvent, error) {
	logging.Logger.Info("StreamConsoleChat called", "token_len", len(token))
	var proxyOpt protocol.ConsoleProxy
	dir, err := getProxyDirectory(ctx)
	if err != nil {
		logging.Logger.Warn("proxy directory unavailable, proceeding without proxy", "error", err)
	} else {
		logging.Logger.Info("proxy directory acquired", "node_count", dir.NodeCount())
		proxyOpt = &consoleProxyAdapter{dir: dir}
	}
	return protocol.StreamConsoleChat(ctx, token, payload, protocol.ConsoleStreamOptions{
		Proxy:    proxyOpt,
		Poster:   consoleStreamPosterFactory(),
		TimeoutS: timeoutS,
	})
}

type consoleProxyAdapter struct {
	dir *controlproxy.ProxyDirectory
}

func (a *consoleProxyAdapter) Acquire(ctx context.Context) (controlproxy.ProxyLease, error) {
	lease, err := a.dir.Acquire(ctx)
	if err != nil {
		logging.Logger.Warn("proxy lease acquire failed", "error", err)
	} else if lease.ProxyURL != nil {
		logging.Logger.Info("proxy lease acquired", "proxy", *lease.ProxyURL)
	} else {
		logging.Logger.Info("proxy lease acquired (direct)")
	}
	return lease, err
}

func (a *consoleProxyAdapter) Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	return a.dir.Feedback(ctx, lease, feedback)
}

type consoleHTTPPoster struct{}

func (consoleHTTPPoster) PostConsoleStream(ctx context.Context, request protocol.ConsoleStreamRequest) (protocol.ConsoleStreamResponse, error) {
	payload, err := json.Marshal(request.Payload)
	if err != nil {
		return protocol.ConsoleStreamResponse{}, err
	}
	endpoint := consoleHTTPEndpoint()
	stream, err := transport.PostStream(ctx, endpoint, request.Token, payload, consoleHTTPOptions(request))
	if err != nil {
		logConsoleTransportError(endpoint, err)
		return protocol.ConsoleStreamResponse{}, err
	}
	defer stream.Close()

	lines := []string{}
	for {
		line, ok, err := stream.Next()
		if err != nil {
			return protocol.ConsoleStreamResponse{}, err
		}
		if !ok {
			return protocol.ConsoleStreamResponse{StatusCode: 200, Lines: lines}, nil
		}
		lines = append(lines, line)
	}
}

func consoleHTTPOptions(request protocol.ConsoleStreamRequest) transport.HTTPOptions {
	timeout := time.Duration(request.TimeoutS * float64(time.Second))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return transport.HTTPOptions{
		Lease:          proxyLeasePtr(request.Lease),
		Timeout:        timeout,
		ContentType:    "application/json",
		ConsoleHeaders: true,
		ExtraHeaders: map[string]string{
			"x-cluster": "https://us-east-1.api.x.ai",
		},
	}
}

func consoleHTTPEndpoint() string {
	return reverseruntime.ConsoleResponses
}

func proxyLeasePtr(lease controlproxy.ProxyLease) *controlproxy.ProxyLease {
	return &lease
}

func logConsoleTransportError(endpoint string, err error) {
	var upstream *platform.UpstreamError
	if errors.As(err, &upstream) {
		body := redactConsoleDiagnosticText(upstream.Body)
		logging.Logger.Warn(
			"console upstream request failed",
			"endpoint", endpoint,
			"status", upstream.Status,
			"body_len", len(upstream.Body),
			"body_sha256", consoleBodyHash(upstream.Body),
			"body_excerpt", truncateConsoleDiagnosticText(body, 400),
		)
		return
	}
	logging.Logger.Warn(
		"console transport request failed",
		"endpoint", endpoint,
		"error", truncateConsoleDiagnosticText(redactConsoleDiagnosticText(err.Error()), 400),
	)
}

func consoleBodyHash(body string) string {
	if body == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", sum[:8])
}

func truncateConsoleDiagnosticText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func redactConsoleDiagnosticText(value string) string {
	out := strings.ReplaceAll(value, "\n", `\n`)
	replacements := []struct {
		pattern *regexp.Regexp
		repl    string
	}{
		{regexp.MustCompile(`(?i)\b(sso|sso-rw|cf_clearance)=([^;\s"'\\]+)`), `${1}=<redacted>`},
		{regexp.MustCompile(`(?i)(bearer\s+)([A-Za-z0-9._~+/=-]{8,})`), `${1}<redacted>`},
		{regexp.MustCompile(`\b[A-Za-z0-9_-]{32,}\b`), `<redacted>`},
	}
	for _, replacement := range replacements {
		out = replacement.pattern.ReplaceAllString(out, replacement.repl)
	}
	return out
}
