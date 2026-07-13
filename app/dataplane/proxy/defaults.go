package proxy

import (
	"context"
	"fmt"
	"strings"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	"github.com/dslzl/gork/app/control/proxy/providers"
	platformconfig "github.com/dslzl/gork/app/platform/config"
)

type globalDirectoryConfig struct{}

func (globalDirectoryConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func (globalDirectoryConfig) GetList(key string, defaultValue []string) []string {
	defaultAny := make([]any, 0, len(defaultValue))
	for _, item := range defaultValue {
		defaultAny = append(defaultAny, item)
	}
	values := platformconfig.GlobalConfig.GetList(key, defaultAny)
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		value := strings.TrimSpace(fmt.Sprint(item))
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (globalDirectoryConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

func ProductionDirectoryOptions() controlproxy.DirectoryOptions {
	cfg := globalDirectoryConfig{}
	return controlproxy.DirectoryOptions{
		Config:         cfg,
		FlareProvider:  providers.FlareSolverrClearanceProvider{Config: cfg},
		ByparrProvider: providers.ByparrClearanceProvider{Config: cfg},
	}
}

func GetTransportRuntime(ctx context.Context) (*controlproxy.ProxyDirectory, error) {
	return controlproxy.GetProxyDirectory(ctx, ProductionDirectoryOptions())
}
