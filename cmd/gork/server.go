package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type gorkHTTPServerOptions struct {
	Address           string
	Handler           http.Handler
	APIKeys           []string
	AllowUnauth       bool
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

func newGorkHTTPServer(options gorkHTTPServerOptions) (*http.Server, error) {
	if err := validatePublicUnauthenticatedListen(options.Address, options.APIKeys, options.AllowUnauth); err != nil {
		return nil, err
	}
	return &http.Server{
		Addr:              options.Address,
		Handler:           options.Handler,
		ReadTimeout:       options.ReadTimeout,
		ReadHeaderTimeout: options.ReadHeaderTimeout,
		IdleTimeout:       options.IdleTimeout,
		MaxHeaderBytes:    options.MaxHeaderBytes,
	}, nil
}

func validatePublicUnauthenticatedListen(address string, apiKeys []string, allowUnauth bool) error {
	if allowUnauth || len(apiKeys) > 0 || !isPublicListenAddress(address) {
		return nil
	}
	return fmt.Errorf("refusing to listen on public address %q without app.api_key; set ALLOW_UNAUTHENTICATED_API=true to override", address)
}

func isPublicListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return !ip.IsLoopback()
}
