package main

import (
	"net/http"
	"testing"
	"time"
)

func TestValidatePublicUnauthenticatedListenRequiresExplicitOverride(t *testing.T) {
	if err := validatePublicUnauthenticatedListen(":8000", nil, false); err == nil {
		t.Fatalf("public listen without API key should fail")
	}
	if err := validatePublicUnauthenticatedListen("0.0.0.0:8000", []string{}, false); err == nil {
		t.Fatalf("wildcard listen without API key should fail")
	}
	if err := validatePublicUnauthenticatedListen("[::]:8000", nil, false); err == nil {
		t.Fatalf("IPv6 wildcard listen without API key should fail")
	}
	if err := validatePublicUnauthenticatedListen("127.0.0.1:8000", nil, false); err != nil {
		t.Fatalf("loopback listen should allow empty API key: %v", err)
	}
	if err := validatePublicUnauthenticatedListen("0.0.0.0:8000", nil, true); err != nil {
		t.Fatalf("explicit unauthenticated override rejected: %v", err)
	}
	if err := validatePublicUnauthenticatedListen("0.0.0.0:8000", []string{"secret"}, false); err != nil {
		t.Fatalf("configured API key rejected: %v", err)
	}
}

func TestNewGorkHTTPServerAppliesSecurityOptions(t *testing.T) {
	options := gorkHTTPServerOptions{
		Address:           "127.0.0.1:0",
		Handler:           http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ReadTimeout:       2 * time.Second,
		IdleTimeout:       3 * time.Second,
		MaxHeaderBytes:    4096,
		ReadHeaderTimeout: 4 * time.Second,
	}

	server, err := newGorkHTTPServer(options)
	if err != nil {
		t.Fatalf("newGorkHTTPServer returned error: %v", err)
	}
	if server.Addr != options.Address || server.Handler == nil {
		t.Fatalf("server address/handler mismatch: %#v", server)
	}
	if server.ReadTimeout != 2*time.Second || server.IdleTimeout != 3*time.Second ||
		server.ReadHeaderTimeout != 4*time.Second || server.MaxHeaderBytes != 4096 {
		t.Fatalf("server timeouts/header bytes = %#v", server)
	}
	if server.WriteTimeout != 0 {
		t.Fatalf("stream endpoints require no global write timeout, got %s", server.WriteTimeout)
	}
}
