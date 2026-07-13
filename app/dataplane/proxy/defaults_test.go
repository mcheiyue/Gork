package proxy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/control/proxy/providers"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type fakeProductionConfigBackend struct {
	data map[string]any
}

func (f fakeProductionConfigBackend) Load(context.Context) (map[string]any, error) {
	return f.data, nil
}

func (f fakeProductionConfigBackend) ApplyPatch(context.Context, map[string]any) error {
	return nil
}

func (f fakeProductionConfigBackend) Clear(context.Context) error {
	return nil
}

func (f fakeProductionConfigBackend) Version(context.Context) (any, error) {
	return "test", nil
}

func (f fakeProductionConfigBackend) Close(context.Context) error {
	return nil
}

func TestProductionDirectoryOptionsUseGlobalConfigAndClearanceProviders(t *testing.T) {
	previous := platformconfig.GlobalConfig
	t.Cleanup(func() { platformconfig.GlobalConfig = previous })

	defaultsPath := filepath.Join(t.TempDir(), "config.defaults.toml")
	if err := os.WriteFile(defaultsPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write defaults: %v", err)
	}
	platformconfig.GlobalConfig = platformconfig.NewConfigSnapshot(fakeProductionConfigBackend{
		data: map[string]any{
			"proxy": map[string]any{
				"egress": map[string]any{
					"mode":      "single_proxy",
					"proxy_url": "http://proxy:8080",
				},
				"clearance": map[string]any{
					"mode":             "byparr",
					"byparr_url":       "http://byparr:8191",
					"flaresolverr_url": "http://solver:8191",
				},
			},
		},
	}, platformconfig.ConfigSnapshotOptions{})
	if err := platformconfig.GlobalConfig.Load(context.Background(), defaultsPath); err != nil {
		t.Fatalf("load global config: %v", err)
	}

	options := ProductionDirectoryOptions()
	if _, ok := options.FlareProvider.(providers.FlareSolverrClearanceProvider); !ok {
		t.Fatalf("FlareProvider = %T, want FlareSolverrClearanceProvider", options.FlareProvider)
	}
	if _, ok := options.ByparrProvider.(providers.ByparrClearanceProvider); !ok {
		t.Fatalf("ByparrProvider = %T, want ByparrClearanceProvider", options.ByparrProvider)
	}
	directory := controlproxy.NewProxyDirectory(options)
	if err := directory.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if directory.EgressMode() != controlproxy.EgressModeSingleProxy || directory.ClearanceMode() != controlproxy.ClearanceModeByparr {
		t.Fatalf("directory modes = %s/%s", directory.EgressMode(), directory.ClearanceMode())
	}
	if directory.NodeCount() != 1 {
		t.Fatalf("NodeCount = %d, want configured proxy node", directory.NodeCount())
	}
}
