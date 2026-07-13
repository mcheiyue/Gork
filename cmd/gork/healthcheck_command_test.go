package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunGorkCommandHealthcheckOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"healthcheck", "--url", server.URL, "--timeout", "1s"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code != 0 || strings.TrimSpace(stdout.String()) != "ok" {
		t.Fatalf("handled/code/stdout/stderr = %t/%d/%q/%q", handled, code, stdout.String(), stderr.String())
	}
}

func TestRunGorkCommandHealthcheckFailsOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	handled, code, err := runGorkCommand(context.Background(), []string{"healthcheck", "--url", server.URL}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("runGorkCommand returned error: %v", err)
	}
	if !handled || code != 1 || !strings.Contains(stderr.String(), "status=503") {
		t.Fatalf("handled/code/stdout/stderr = %t/%d/%q/%q", handled, code, stdout.String(), stderr.String())
	}
}

func TestRunGorkCommandHealthcheckDefaultURLUsesEnvironment(t *testing.T) {
	t.Setenv("GORK_HEALTHCHECK_URL", "")
	t.Setenv("PORT", "19090")
	t.Setenv("SERVER_PORT", "18080")
	if got := defaultHealthcheckURL(); got != "http://127.0.0.1:19090/health" {
		t.Fatalf("defaultHealthcheckURL with PORT = %q", got)
	}
	t.Setenv("PORT", "")
	if got := defaultHealthcheckURL(); got != "http://127.0.0.1:18080/health" {
		t.Fatalf("defaultHealthcheckURL with SERVER_PORT = %q", got)
	}
	t.Setenv("GORK_HEALTHCHECK_URL", "http://example.test/healthz")
	if got := defaultHealthcheckURL(); got != "http://example.test/healthz" {
		t.Fatalf("defaultHealthcheckURL override = %q", got)
	}
}

func TestRunGorkCommandHealthcheckRejectsUnknownFlag(t *testing.T) {
	handled, code, err := runGorkCommand(context.Background(), []string{"healthcheck", "--bad"}, &bytes.Buffer{}, &bytes.Buffer{})
	if !handled || code != 2 || err == nil {
		t.Fatalf("handled/code/err = %t/%d/%v", handled, code, err)
	}
}
