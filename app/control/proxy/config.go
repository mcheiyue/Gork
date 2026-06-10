package proxy

import (
	"strings"

	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type ClearanceConfig struct {
	CFCookies   string
	UserAgent   string
	CFClearance string
	Browser     string
}

type StringConfig interface {
	GetString(key, defaultValue string) string
}

type emptyStringConfig struct{}

func (emptyStringConfig) GetString(_ string, defaultValue string) string {
	return defaultValue
}

type globalStringConfig struct{}

func (globalStringConfig) GetString(key, defaultValue string) string {
	return platformconfig.GlobalConfig.GetStr(key, defaultValue)
}

func configString(cfg StringConfig, key string) string {
	value := cfg.GetString(key, "")
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
}

func FirstConfigString(cfg StringConfig, keys ...string) string {
	if cfg == nil {
		cfg = emptyStringConfig{}
	}
	for _, key := range keys {
		value := configString(cfg, key)
		if value != "" {
			return value
		}
	}
	return ""
}

func ResolveClearanceConfig(cfg StringConfig) ClearanceConfig {
	if cfg == nil {
		cfg = globalStringConfig{}
	}
	return ClearanceConfig{
		CFCookies: FirstConfigString(
			cfg,
			"proxy.cf_cookies",
			"proxy.clearance.cf_cookies",
		),
		UserAgent: FirstConfigString(
			cfg,
			"proxy.user_agent",
			"proxy.clearance.user_agent",
		),
		CFClearance: FirstConfigString(
			cfg,
			"proxy.cf_clearance",
			"proxy.clearance.cf_clearance",
		),
		Browser: FirstConfigString(
			cfg,
			"proxy.browser",
			"proxy.clearance.browser",
		),
	}
}
