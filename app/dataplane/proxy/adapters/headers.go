package adapters

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	mathrand "math/rand"
	"net/url"
	"regexp"
	"strings"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type CookieOptions struct {
	Lease       *controlproxy.ProxyLease
	CFCookies   *string
	CFClearance *string
}

type HTTPHeaderOptions struct {
	ContentType *string
	Origin      string
	Referer     string
	Lease       *controlproxy.ProxyLease
}

type WSHeaderOptions struct {
	Origin string
	Extra  map[string]string
	Lease  *controlproxy.ProxyLease
}

type ConsoleHeaderOptions struct {
	Lease       *controlproxy.ProxyLease
	ContentType string
}

func sanitize(value *string, field string, stripSpaces bool) string {
	raw := ""
	if value != nil {
		raw = *value
	}
	_ = field

	translated := strings.Map(normalizeHeaderRune, raw)
	if stripSpaces {
		translated = regexp.MustCompile(`\s+`).ReplaceAllString(translated, "")
	} else {
		translated = strings.TrimSpace(translated)
	}
	return strings.Map(func(r rune) rune {
		if r <= 0xff {
			return r
		}
		return -1
	}, translated)
}

func normalizeHeaderRune(r rune) rune {
	switch r {
	case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2212':
		return '-'
	case '\u2018', '\u2019':
		return '\''
	case '\u201c', '\u201d':
		return '"'
	case '\u00a0', '\u2007', '\u202f':
		return ' '
	case '\u200b', '\u200c', '\u200d', '\ufeff':
		return -1
	default:
		return r
	}
}

func statsigID() string {
	if platformconfig.GlobalConfig != nil && !platformconfig.GlobalConfig.GetBool("features.dynamic_statsig", true) {
		return uuidString()
	}
	if mathrand.Intn(2) == 0 {
		msg := fmt.Sprintf("x1:TypeError: Cannot read properties of null (reading 'children[\\'%s\\']')", randomString("abcdefghijklmnopqrstuvwxyz0123456789", 5))
		return base64.StdEncoding.EncodeToString([]byte(msg))
	}
	msg := fmt.Sprintf("x1:TypeError: Cannot read properties of undefined (reading '%s')", randomString("abcdefghijklmnopqrstuvwxyz", 10))
	return base64.StdEncoding.EncodeToString([]byte(msg))
}

func majorVersion(browser, ua string) string {
	for _, src := range []string{browser, ua} {
		if match := regexp.MustCompile(`(\d{2,3})`).FindStringSubmatch(src); len(match) > 0 {
			return match[1]
		}
	}
	return ""
}

func platformFromUA(ua string) string {
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "mac os x") || strings.Contains(lower, "macintosh"):
		return "macOS"
	case strings.Contains(lower, "android"):
		return "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad"):
		return "iOS"
	case strings.Contains(lower, "linux"):
		return "Linux"
	default:
		return ""
	}
}

func archFromUA(ua string) string {
	lower := strings.ToLower(ua)
	if strings.Contains(lower, "aarch64") || strings.Contains(lower, "arm") {
		return "arm"
	}
	if strings.Contains(lower, "x86_64") || strings.Contains(lower, "x64") ||
		strings.Contains(lower, "win64") || strings.Contains(lower, "intel") {
		return "x86"
	}
	return ""
}

func clientHints(browser, ua string) map[string]string {
	lowerBrowser := strings.ToLower(browser)
	lowerUA := strings.ToLower(ua)
	isChromium := containsAny(lowerBrowser, "chrome", "chromium", "edge", "brave") ||
		containsAny(lowerUA, "chrome", "chromium", "edg")
	if !isChromium || strings.Contains(lowerUA, "firefox") ||
		(strings.Contains(lowerUA, "safari") && !strings.Contains(lowerUA, "chrome")) {
		return map[string]string{}
	}
	version := majorVersion(browser, ua)
	if version == "" {
		return map[string]string{}
	}

	brand := "Google Chrome"
	switch {
	case strings.Contains(lowerBrowser, "edge") || strings.Contains(lowerUA, "edg"):
		brand = "Microsoft Edge"
	case strings.Contains(lowerBrowser, "brave"):
		brand = "Brave"
	case strings.Contains(lowerBrowser, "chromium"):
		brand = "Chromium"
	}

	platform := platformFromUA(ua)
	arch := archFromUA(ua)
	mobile := "?0"
	if strings.Contains(lowerUA, "mobile") || platform == "Android" || platform == "iOS" {
		mobile = "?1"
	}

	hints := map[string]string{
		"Sec-Ch-Ua":        fmt.Sprintf("\"%s\";v=\"%s\", \"Chromium\";v=\"%s\", \"Not(A:Brand\";v=\"24\"", brand, version, version),
		"Sec-Ch-Ua-Mobile": mobile,
		"Sec-Ch-Ua-Model":  "",
	}
	if platform != "" {
		hints["Sec-Ch-Ua-Platform"] = fmt.Sprintf("\"%s\"", platform)
	}
	if arch != "" {
		hints["Sec-Ch-Ua-Arch"] = arch
		hints["Sec-Ch-Ua-Bitness"] = "64"
	}
	return hints
}

func resolveProfile(lease *controlproxy.ProxyLease) ProxyProfile {
	return ResolveProxyProfile(lease)
}

func BuildSSOCookie(ssoToken string, options ...CookieOptions) string {
	opts := CookieOptions{}
	if len(options) > 0 {
		opts = options[0]
	}

	token := ssoToken
	if strings.HasPrefix(token, "sso=") {
		token = token[4:]
	}
	token = sanitize(&token, "sso_token", true)
	cookie := fmt.Sprintf("sso=%s; sso-rw=%s", token, token)

	profile := resolveProfile(opts.Lease)
	cfCookies := profile.CFCookies
	if opts.CFCookies != nil {
		cfCookies = *opts.CFCookies
	}
	cfClearance := profile.CFClearance
	if opts.CFClearance != nil {
		cfClearance = *opts.CFClearance
	}
	effectiveCookies := sanitize(&cfCookies, "cf_cookies", false)
	effectiveClearance := sanitize(&cfClearance, "cf_clearance", true)

	if effectiveClearance != "" && effectiveCookies != "" {
		if regexp.MustCompile(`(?:^|;\s*)cf_clearance=`).MatchString(effectiveCookies) {
			effectiveCookies = replaceFirstCFClearance(effectiveCookies, effectiveClearance)
		} else {
			effectiveCookies = strings.TrimRight(effectiveCookies, "; ") + "; cf_clearance=" + effectiveClearance
		}
	} else if effectiveClearance != "" {
		effectiveCookies = "cf_clearance=" + effectiveClearance
	}

	if effectiveCookies != "" {
		cookie += "; " + effectiveCookies
	}
	return cookie
}

func BuildHTTPHeaders(cookieToken string, options ...HTTPHeaderOptions) map[string]string {
	opts := HTTPHeaderOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	profile := resolveProfile(opts.Lease)
	rawUA := profile.UserAgent
	ua := sanitize(&rawUA, "user_agent", false)
	browser := profile.Browser
	origin := opts.Origin
	if origin == "" {
		origin = "https://grok.com"
	}
	referer := opts.Referer
	if referer == "" {
		referer = "https://grok.com/"
	}
	origin = sanitize(&origin, "origin", false)
	referer = sanitize(&referer, "referer", false)

	contentType := "application/json"
	if opts.ContentType != nil && *opts.ContentType != "" {
		contentType = *opts.ContentType
	}
	accept := "*/*"
	fetchDest := "empty"
	switch contentType {
	case "image/jpeg", "image/png", "video/mp4", "video/webm":
		accept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"
		fetchDest = "document"
	}

	site := "same-site"
	if originHost(origin) != "" && originHost(origin) == originHost(referer) {
		site = "same-origin"
	}
	headers := map[string]string{
		"Accept":           accept,
		"Accept-Encoding":  "gzip, deflate, br, zstd",
		"Accept-Language":  "zh-CN,zh;q=0.9,en;q=0.8",
		"Baggage":          "sentry-environment=production,sentry-release=d6add6fb0460641fd482d767a335ef72b9b6abb8,sentry-public_key=b311e0f2690c81f25e2c4cf6d4f7ce1c",
		"Content-Type":     contentType,
		"Origin":           origin,
		"Priority":         "u=1, i",
		"Referer":          referer,
		"Sec-Fetch-Dest":   fetchDest,
		"Sec-Fetch-Mode":   "cors",
		"Sec-Fetch-Site":   site,
		"User-Agent":       ua,
		"x-statsig-id":     statsigID(),
		"x-xai-request-id": uuidString(),
	}
	for key, value := range clientHints(browser, rawUA) {
		headers[key] = value
	}
	headers["Cookie"] = BuildSSOCookie(cookieToken, CookieOptions{Lease: opts.Lease})
	return headers
}

func BuildWSHeaders(token string, options ...WSHeaderOptions) map[string]string {
	opts := WSHeaderOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	profile := resolveProfile(opts.Lease)
	rawUA := profile.UserAgent
	ua := sanitize(&rawUA, "user_agent", false)
	browser := profile.Browser
	origin := opts.Origin
	if origin == "" {
		origin = "https://grok.com"
	}
	origin = sanitize(&origin, "origin", false)

	headers := map[string]string{
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Cache-Control":   "no-cache",
		"Origin":          origin,
		"Pragma":          "no-cache",
		"User-Agent":      ua,
	}
	for key, value := range clientHints(browser, rawUA) {
		headers[key] = value
	}
	if token != "" {
		headers["Cookie"] = BuildSSOCookie(token, CookieOptions{Lease: opts.Lease})
	}
	for key, value := range opts.Extra {
		headers[key] = value
	}
	return headers
}

func BuildConsoleHeaders(ssoToken string, options ...ConsoleHeaderOptions) map[string]string {
	opts := ConsoleHeaderOptions{ContentType: "application/json"}
	if len(options) > 0 {
		opts = options[0]
		if opts.ContentType == "" {
			opts.ContentType = "application/json"
		}
	}
	token := ssoToken
	if strings.HasPrefix(token, "sso=") {
		token = token[4:]
	}
	token = sanitize(&token, "sso_token", true)
	profile := resolveProfile(opts.Lease)
	ua := sanitize(&profile.UserAgent, "user_agent", false)
	cfClearance := sanitize(&profile.CFClearance, "cf_clearance", true)

	cookie := fmt.Sprintf("sso=%s; sso-rw=%s", token, token)
	if cfClearance != "" {
		cookie += "; cf_clearance=" + cfClearance
	}
	if ua == "" {
		ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	}
	headers := map[string]string{
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate, br, zstd",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Authorization":   "Bearer anonymous",
		"Content-Type":    opts.ContentType,
		"Cookie":          cookie,
		"Origin":          "https://console.x.ai",
		"Priority":        "u=1, i",
		"Referer":         "https://console.x.ai/",
		"Sec-Fetch-Dest":  "empty",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Site":  "same-origin",
		"User-Agent":      ua,
		"x-cluster":       "https://us-east-1.api.x.ai",
	}
	for key, value := range clientHints(profile.Browser, profile.UserAgent) {
		headers[key] = value
	}
	return headers
}

func replaceFirstCFClearance(cookies string, clearance string) string {
	re := regexp.MustCompile(`(^|;\s*)cf_clearance=[^;]*`)
	return re.ReplaceAllString(cookies, "${1}cf_clearance="+escapeReplacement(clearance))
}

func escapeReplacement(value string) string {
	return strings.ReplaceAll(value, "$", "$$")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func originHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func randomString(alphabet string, length int) string {
	if length <= 0 {
		return ""
	}
	var builder strings.Builder
	for i := 0; i < length; i++ {
		builder.WriteByte(alphabet[mathrand.Intn(len(alphabet))])
	}
	return builder.String()
}

func uuidString() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fallbackUUID()
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	)
}

func fallbackUUID() string {
	parts := []int{8, 4, 4, 4, 12}
	out := make([]string, 0, len(parts))
	for _, length := range parts {
		out = append(out, randomString("0123456789abcdef", length))
	}
	return strings.Join(out, "-")
}
