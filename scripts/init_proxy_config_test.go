package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProxyConfigCreatesMissingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "data", "config.toml")
	var out bytes.Buffer

	if err := initProxyConfig(configPath, &out); err != nil {
		t.Fatalf("initProxyConfig() error = %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", configPath, err)
	}
	if want := expectedProxyConfig(); string(got) != want {
		t.Fatalf("created config mismatch\nwant:\n%s\ngot:\n%s", want, string(got))
	}
	if gotMsg, wantMsg := out.String(), "[init-config] Created config.toml with proxy settings\n"; gotMsg != wantMsg {
		t.Fatalf("stdout mismatch: want %q, got %q", wantMsg, gotMsg)
	}
}

func TestInitProxyConfigUpdatesConfigWithoutPrivoxy(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	original := "title = \"before\"\n\n[proxy.egress]\nmode = \"disabled\"\nurl = \"http://old-proxy\"\n[proxy.clearance]\nmode = \"none\"\n[server]\nport = 8080\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}
	var out bytes.Buffer

	if err := initProxyConfig(configPath, &out); err != nil {
		t.Fatalf("initProxyConfig() error = %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", configPath, err)
	}
	want := "title = \"before\"\n\n[server]\nport = 8080\n\n" + strings.TrimSuffix(expectedProxyConfig(), "\n") + "\n"
	if string(got) != want {
		t.Fatalf("updated config mismatch\nwant:\n%s\ngot:\n%s", want, string(got))
	}
	if strings.Contains(string(got), "mode = \"disabled\"") || strings.Contains(string(got), "mode = \"none\"") {
		t.Fatalf("old proxy sections were not removed:\n%s", string(got))
	}
	if gotMsg, wantMsg := out.String(), "[init-config] Updated config.toml with proxy settings\n"; gotMsg != wantMsg {
		t.Fatalf("stdout mismatch: want %q, got %q", wantMsg, gotMsg)
	}
}

func TestInitProxyConfigSkipsConfigAlreadyContainingPrivoxy(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	original := "[proxy.egress]\nproxy_url = \"http://privoxy:8118\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", configPath, err)
	}
	var out bytes.Buffer

	if err := initProxyConfig(configPath, &out); err != nil {
		t.Fatalf("initProxyConfig() error = %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", configPath, err)
	}
	if string(got) != original {
		t.Fatalf("existing config changed\nwant:\n%s\ngot:\n%s", original, string(got))
	}
	if gotMsg, wantMsg := out.String(), "[init-config] Proxy settings already present, skipping\n"; gotMsg != wantMsg {
		t.Fatalf("stdout mismatch: want %q, got %q", wantMsg, gotMsg)
	}
}
