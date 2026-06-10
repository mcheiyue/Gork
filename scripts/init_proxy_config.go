package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const defaultConfigPath = "/app/data/config.toml"

const proxyConfig = `
[proxy.egress]
mode = "single_proxy"
proxy_url = "http://privoxy:8118"
resource_proxy_url = "http://privoxy:8118"
proxy_pool = []
resource_proxy_pool = []
skip_ssl_verify = false

[proxy.clearance]
mode = "flaresolverr"
cf_cookies = ""
user_agent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
browser = "chrome136"
flaresolverr_url = "http://flaresolverr:8191"
timeout_sec = 60
refresh_interval = 3600
`

func expectedProxyConfig() string {
	return strings.TrimSpace(proxyConfig) + "\n"
}

func initProxyConfig(configPath string, stdout io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	content, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(expectedProxyConfig()), 0o644); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "[init-config] Created config.toml with proxy settings")
		return err
	}
	if err != nil {
		return err
	}

	if strings.Contains(string(content), "privoxy") {
		_, err = fmt.Fprintln(stdout, "[init-config] Proxy settings already present, skipping")
		return err
	}

	updated := string(content)
	updated = removeProxySection(updated, "[proxy.egress]")
	updated = removeProxySection(updated, "[proxy.clearance]")
	updated += "\n" + strings.TrimSpace(proxyConfig) + "\n"
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "[init-config] Updated config.toml with proxy settings")
	return err
}

func removeProxySection(content, header string) string {
	start := strings.Index(content, header)
	if start == -1 {
		return content
	}
	rest := content[start+len(header):]
	next := strings.Index(rest, "[")
	if next == -1 {
		return content[:start]
	}
	return content[:start] + rest[next:]
}

func main() {
	if err := initProxyConfig(defaultConfigPath, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
