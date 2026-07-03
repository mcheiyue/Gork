package openai

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/platform/config"
)

const routerMediaDefaultTTL = time.Hour

var (
	routerMediaNow           = time.Now
	routerMediaSigningSecret = defaultRouterMediaSigningSecret

	routerMediaProcessSecretOnce sync.Once
	routerMediaProcessSecret     string
)

func signedRouterFileURL(routePath string, fileID string) string {
	exp := routerMediaNow().Add(routerMediaTTL()).Unix()
	values := url.Values{}
	values.Set("id", fileID)
	values.Set("exp", strconv.FormatInt(exp, 10))
	values.Set("sig", routerMediaSignature(fileID, exp))
	return routePath + "?" + values.Encode()
}

func routerMediaSignature(fileID string, exp int64) string {
	mac := hmac.New(sha256.New, []byte(routerMediaSigningSecret()))
	_, _ = fmt.Fprintf(mac, "%s:%d", fileID, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

func routerMediaSignatureValid(fileID string, exp int64, sig string) bool {
	expected := routerMediaSignature(fileID, exp)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(strings.TrimSpace(sig))) == 1
}

func defaultRouterMediaSigningSecret() string {
	for _, key := range []string{
		"security.media.signing_secret",
		"app.app_key",
		"app.api_key",
	} {
		value := strings.TrimSpace(fmt.Sprint(config.GetConfig(key, "")))
		if value != "" {
			return value
		}
	}
	return routerMediaFallbackProcessSecret()
}

func routerMediaFallbackProcessSecret() string {
	routerMediaProcessSecretOnce.Do(func() {
		var raw [32]byte
		if _, err := rand.Read(raw[:]); err != nil {
			routerMediaProcessSecret = strconv.FormatInt(time.Now().UnixNano(), 36)
			return
		}
		routerMediaProcessSecret = hex.EncodeToString(raw[:])
	})
	return routerMediaProcessSecret
}

func routerMediaTTL() time.Duration {
	value := config.GetConfig("security.media.signed_url_ttl_seconds", int(routerMediaDefaultTTL.Seconds()))
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	case int64:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	case float64:
		if typed > 0 {
			return time.Duration(typed) * time.Second
		}
	case string:
		seconds, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return routerMediaDefaultTTL
}
