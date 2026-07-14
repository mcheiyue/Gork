package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
	"github.com/dslzl/gork/app/dataplane/build"
)

type stubBuildDir struct {
	accounts []buildaccount.Account
	status   []string
}

func (s *stubBuildDir) ListActive(context.Context, time.Time) ([]buildaccount.Account, error) {
	return s.accounts, nil
}
func (s *stubBuildDir) UpdateTokens(context.Context, int64, string, string, time.Time) error {
	return nil
}
func (s *stubBuildDir) SetStatus(_ context.Context, id int64, status, reason string) error {
	s.status = append(s.status, fmt.Sprintf("%d:%s:%s", id, status, reason))
	return nil
}

type stubBuildHTTP struct {
	status int
	body   string
}

func (s *stubBuildHTTP) CreateResponse(context.Context, build.RequestMeta, io.Reader) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
	}, nil
}

type stubOAuth struct{}

func (stubOAuth) Refresh(context.Context, string) (build.TokenPayload, error) {
	return build.TokenPayload{}, &build.RefreshError{Code: "invalid_grant", Permanent: true}
}

func TestBuildCompletionsFeatureOffUnknownModel(t *testing.T) {
	prev := buildFeatureEnabled
	buildFeatureEnabled = func() bool { return false }
	defer func() { buildFeatureEnabled = prev }()

	_, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "Unknown model") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildCompletionsHappyPath(t *testing.T) {
	prevF := buildFeatureEnabled
	prevD := buildAccountDir
	prevC := buildAPIClient
	prevO := buildOAuthClient
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory {
		return &stubBuildDir{accounts: []buildaccount.Account{{
			ID: 1, AccessToken: "at", UserID: "u1", Status: buildaccount.StatusActive,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}}}
	}
	buildAPIClient = func() buildHTTPClient {
		return &stubBuildHTTP{status: 200, body: `{"output_text":"pong"}`}
	}
	buildOAuthClient = func() buildTokenRefresher { return stubOAuth{} }
	defer func() {
		buildFeatureEnabled = prevF
		buildAccountDir = prevD
		buildAPIClient = prevC
		buildOAuthClient = prevO
	}()

	got, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	choices := got.Response["choices"].([]map[string]any)
	msg := choices[0]["message"].(map[string]any)
	if msg["content"] != "pong" {
		t.Fatalf("%#v", got.Response)
	}
}

func TestBuildCompletionsNoAccounts(t *testing.T) {
	prevF := buildFeatureEnabled
	prevD := buildAccountDir
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory { return &stubBuildDir{} }
	defer func() {
		buildFeatureEnabled = prevF
		buildAccountDir = prevD
	}()

	_, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildCompletionsSkipsExhaustedBilling(t *testing.T) {
	prevF := buildFeatureEnabled
	prevD := buildAccountDir
	prevC := buildAPIClient
	prevO := buildOAuthClient
	dir := &stubBuildDir{accounts: []buildaccount.Account{
		{
			ID: 1, AccessToken: "dead", UserID: "u-dead", Status: buildaccount.StatusActive,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
			Billing:   build.Billing{SyncedAt: time.Now().UTC(), MonthlyLimit: 10, Used: 10},
		},
		{
			ID: 2, AccessToken: "ok", UserID: "u-ok", Status: buildaccount.StatusActive,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		},
	}}
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory { return dir }
	buildAPIClient = func() buildHTTPClient {
		return &stubBuildHTTP{status: 200, body: `{"output_text":"from-ok"}`}
	}
	buildOAuthClient = func() buildTokenRefresher { return stubOAuth{} }
	defer func() {
		buildFeatureEnabled = prevF
		buildAccountDir = prevD
		buildAPIClient = prevC
		buildOAuthClient = prevO
	}()

	got, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	choices := got.Response["choices"].([]map[string]any)
	msg := choices[0]["message"].(map[string]any)
	if msg["content"] != "from-ok" {
		t.Fatalf("%#v", got.Response)
	}
	if len(dir.status) != 1 || !strings.Contains(dir.status[0], "1:cooling:billing") {
		t.Fatalf("status=%v", dir.status)
	}
}

func TestBuildCompletionsStreamFrames(t *testing.T) {
	prevF := buildFeatureEnabled
	prevD := buildAccountDir
	prevC := buildAPIClient
	prevO := buildOAuthClient
	buildFeatureEnabled = func() bool { return true }
	buildAccountDir = func() buildAccountDirectory {
		return &stubBuildDir{accounts: []buildaccount.Account{{
			ID: 1, AccessToken: "at", UserID: "u1", Status: buildaccount.StatusActive,
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}}}
	}
	sseBody := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"
	buildAPIClient = func() buildHTTPClient {
		return &stubBuildHTTP{status: 200, body: sseBody}
	}
	buildOAuthClient = func() buildTokenRefresher { return stubOAuth{} }
	defer func() {
		buildFeatureEnabled = prevF
		buildAccountDir = prevD
		buildAPIClient = prevC
		buildOAuthClient = prevO
	}()
	stream := true
	got, err := Completions(context.Background(), chatCompletionOptions{
		Model:    "build/grok-4",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
		Stream:   &stream,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsStream || len(got.StreamFrames) < 2 {
		t.Fatalf("%#v", got)
	}
	joined := strings.Join(got.StreamFrames, "")
	if !strings.Contains(joined, "chat.completion.chunk") || !strings.Contains(joined, "[DONE]") {
		t.Fatalf("%s", joined)
	}
}
