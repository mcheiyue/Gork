package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFlattenAndDeepMergeConfigMatchPython(t *testing.T) {
	base := map[string]any{
		"proxy": map[string]any{"url": "base", "timeout": int64(10)},
		"app":   map[string]any{"name": "grok"},
	}
	override := map[string]any{
		"proxy": map[string]any{"timeout": int64(20), "mode": "pool"},
	}

	merged := DeepMergeConfig(base, override)
	want := map[string]any{
		"proxy": map[string]any{"url": "base", "timeout": int64(20), "mode": "pool"},
		"app":   map[string]any{"name": "grok"},
	}
	if !reflect.DeepEqual(want, merged) {
		t.Fatalf("merged config mismatch\nwant: %#v\n got: %#v", want, merged)
	}
	if base["proxy"].(map[string]any)["timeout"] != int64(10) {
		t.Fatalf("DeepMergeConfig mutated base: %#v", base)
	}

	flat := FlattenConfig(merged, "")
	wantFlat := map[string]any{
		"proxy.url":     "base",
		"proxy.timeout": int64(20),
		"proxy.mode":    "pool",
		"app.name":      "grok",
	}
	if !reflect.DeepEqual(wantFlat, flat) {
		t.Fatalf("flat config mismatch\nwant: %#v\n got: %#v", wantFlat, flat)
	}
}

func TestLoadConfigAppliesDefaultsUserAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	defaults := filepath.Join(dir, "defaults.toml")
	user := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(defaults, []byte(`
[proxy]
base_proxy_url = "default"
timeout = 10

[app]
webui_enabled = false
`), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	if err := os.WriteFile(user, []byte(`
[proxy]
timeout = 20
mode = "pool"
`), 0o600); err != nil {
		t.Fatalf("write user: %v", err)
	}

	cfg, err := LoadConfig(defaults, LoadConfigOptions{
		UserPath:  user,
		EnvPrefix: "GROK_",
		Env: map[string]string{
			"GROK_PROXY_BASE_PROXY_URL": "env-proxy",
			"GROK_APP_WEBUI_ENABLED":    "true",
			"OTHER_PROXY_TIMEOUT":       "ignored",
		},
	})
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if GetNested(cfg, "proxy.base_proxy_url", "") != "env-proxy" {
		t.Fatalf("proxy base override mismatch: %#v", cfg)
	}
	if GetNested(cfg, "proxy.timeout", nil) != int64(20) || GetNested(cfg, "proxy.mode", "") != "pool" {
		t.Fatalf("user merge mismatch: %#v", cfg)
	}
	if GetNested(cfg, "app.webui_enabled", nil) != "true" {
		t.Fatalf("env values should remain strings: %#v", cfg)
	}
}

func TestLoadTOMLRoundTripsArraysLikePythonTomllib(t *testing.T) {
	path := filepath.Join(t.TempDir(), "defaults.toml")
	if err := os.WriteFile(path, []byte(`
[proxy.egress]
proxy_pool = []
resource_proxy_pool = ["http://a", "http://b"]
reset_session_status_codes = [403]
flags = [true, false]
`), 0o600); err != nil {
		t.Fatalf("write TOML: %v", err)
	}

	cfg, err := LoadTOML(path)
	if err != nil {
		t.Fatalf("LoadTOML returned error: %v", err)
	}
	want := map[string]any{
		"proxy": map[string]any{
			"egress": map[string]any{
				"proxy_pool":                 []any{},
				"resource_proxy_pool":        []any{"http://a", "http://b"},
				"reset_session_status_codes": []any{int64(403)},
				"flags":                      []any{true, false},
			},
		},
	}
	if !reflect.DeepEqual(want, cfg) {
		t.Fatalf("array TOML mismatch\nwant: %#v\n got: %#v", want, cfg)
	}
}

func TestLoadTOMLMissingAndGetNestedDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	cfg, err := LoadTOML(missing)
	if err != nil {
		t.Fatalf("LoadTOML missing returned error: %v", err)
	}
	if len(cfg) != 0 {
		t.Fatalf("missing TOML config = %#v", cfg)
	}

	data := map[string]any{"app": map[string]any{"name": "grok"}, "scalar": "x"}
	if GetNested(data, "app.name", "fallback") != "grok" {
		t.Fatalf("existing nested lookup failed")
	}
	if GetNested(data, "app.missing", "fallback") != "fallback" {
		t.Fatalf("missing lookup did not return default")
	}
	if GetNested(data, "scalar.name", "fallback") != "fallback" {
		t.Fatalf("scalar traversal did not return default")
	}
}
