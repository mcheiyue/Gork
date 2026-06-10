package adapters

import (
	"regexp"
	"strings"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

type ProxyProfile struct {
	CFCookies   string
	UserAgent   string
	CFClearance string
	Browser     string
}

var supportedBrowserProfiles = map[string]struct{}{}

func ExtractCookieValue(cookieHeader, name string) string {
	if cookieHeader == "" {
		return ""
	}
	pattern := regexp.MustCompile(`(?:^|;\s*)` + regexp.QuoteMeta(name) + `=([^;]*)`)
	match := pattern.FindStringSubmatch(cookieHeader)
	if len(match) == 0 {
		return ""
	}
	return match[1]
}

func supportedBrowser(candidate string) string {
	if candidate == "" {
		return ""
	}
	if len(supportedBrowserProfiles) == 0 {
		return candidate
	}
	if _, ok := supportedBrowserProfiles[candidate]; ok {
		return candidate
	}
	family := regexp.MustCompile(`^[a-z_]+`).FindString(candidate)
	if family == "" {
		return ""
	}
	if _, ok := supportedBrowserProfiles[family]; ok {
		return family
	}
	return ""
}

func BrowserFromUserAgent(userAgent string) string {
	lower := strings.ToLower(userAgent)

	if match := regexp.MustCompile(`firefox/(\d+)`).FindStringSubmatch(lower); len(match) > 0 {
		return firstNonEmpty(supportedBrowser("firefox"+match[1]), supportedBrowser("firefox"))
	}

	if match := regexp.MustCompile(`edg/(\d+)`).FindStringSubmatch(lower); len(match) > 0 {
		return firstNonEmpty(supportedBrowser("edge"+match[1]), supportedBrowser("edge"))
	}

	if match := regexp.MustCompile(`(?:chrome|chromium|crios)/(\d+)`).FindStringSubmatch(lower); len(match) > 0 {
		suffix := ""
		if strings.Contains(lower, "android") {
			suffix = "_android"
		}
		exact := supportedBrowser("chrome" + match[1] + suffix)
		fallback := "chrome"
		if suffix != "" {
			fallback = "chrome_android"
		}
		return firstNonEmpty(exact, supportedBrowser(fallback))
	}

	safari := strings.Contains(lower, "safari/") &&
		!strings.Contains(lower, "chrome/") &&
		!strings.Contains(lower, "chromium/")
	if safari {
		if strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") {
			return supportedBrowser("safari_ios")
		}
		return supportedBrowser("safari")
	}

	return ""
}

func ResolveProxyProfile(lease *controlproxy.ProxyLease, configs ...controlproxy.ClearanceConfig) ProxyProfile {
	cfg := controlproxy.ResolveClearanceConfig(nil)
	if len(configs) > 0 {
		cfg = configs[0]
	}

	cookies := cfg.CFCookies
	userAgent := cfg.UserAgent
	clearance := cfg.CFClearance
	if lease != nil {
		if lease.CFCookies != "" {
			cookies = lease.CFCookies
		}
		if lease.UserAgent != "" {
			userAgent = lease.UserAgent
		}
		if value := ExtractCookieValue(lease.CFCookies, "cf_clearance"); value != "" {
			clearance = value
		}
	}

	browser := firstNonEmpty(
		BrowserFromUserAgent(userAgent),
		supportedBrowser(cfg.Browser),
		"chrome120",
	)
	return ProxyProfile{
		CFCookies:   cookies,
		UserAgent:   userAgent,
		CFClearance: clearance,
		Browser:     browser,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
