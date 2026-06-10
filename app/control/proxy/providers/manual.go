package providers

import (
	"fmt"

	"github.com/jiujiu532/grok2api/app/control/proxy"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type ManualClearanceProvider struct {
	Config proxy.StringConfig
}

type globalManualConfig struct{}

func (globalManualConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func (p ManualClearanceProvider) BuildBundle(affinityKey string, clearanceHost ...string) (proxy.ClearanceBundle, bool, error) {
	cfg := p.Config
	if cfg == nil {
		cfg = globalManualConfig{}
	}
	modeValue := cfg.GetString("proxy.clearance.mode", "none")

	mode, err := proxy.ParseClearanceMode(modeValue)
	if err != nil {
		return proxy.ClearanceBundle{}, false, err
	}
	if mode != proxy.ClearanceModeManual {
		return proxy.ClearanceBundle{}, false, nil
	}

	host := "grok.com"
	if len(clearanceHost) > 0 {
		host = clearanceHost[0]
	}

	clearance := proxy.ResolveClearanceConfig(cfg)
	bundle := proxy.NewClearanceBundle(fmt.Sprintf("manual:%s@%s", affinityKey, host))
	bundle.CFCookies = clearance.CFCookies
	bundle.UserAgent = clearance.UserAgent
	bundle.AffinityKey = affinityKey
	bundle.ClearanceHost = host
	return bundle, true, nil
}
