package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func runHealthcheckCommand(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) (bool, int, error) {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	url := flags.String("url", defaultHealthcheckURL(), "healthcheck URL")
	timeout := flags.Duration("timeout", 5*time.Second, "request timeout")
	if err := flags.Parse(args); err != nil {
		return true, 2, err
	}
	if flags.NArg() != 0 {
		return true, 2, fmt.Errorf("unexpected healthcheck argument: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(*url) == "" {
		return true, 2, fmt.Errorf("healthcheck URL cannot be empty")
	}
	client := &http.Client{Timeout: *timeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, *url, nil)
	if err != nil {
		return true, 1, err
	}
	response, err := client.Do(request)
	if err != nil {
		fmt.Fprintf(stderr, "healthcheck failed: %v\n", err)
		return true, 1, nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintf(stderr, "healthcheck failed: status=%d\n", response.StatusCode)
		return true, 1, nil
	}
	fmt.Fprintln(stdout, "ok")
	return true, 0, nil
}

func defaultHealthcheckURL() string {
	if raw := strings.TrimSpace(os.Getenv("GORK_HEALTHCHECK_URL")); raw != "" {
		return raw
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = os.Getenv("SERVER_PORT")
	}
	if port == "" {
		port = "8000"
	}
	return "http://127.0.0.1:" + port + "/health"
}
