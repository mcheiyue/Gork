package openai

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSignedRouterFileURLContainsIDExpSig(t *testing.T) {
	routerMediaNow = func() time.Time { return time.Unix(1700000000, 0) }
	routerMediaSigningSecret = func() string { return "test-secret" }
	t.Cleanup(func() {
		routerMediaNow = time.Now
		routerMediaSigningSecret = defaultRouterMediaSigningSecret
	})

	signed := signedRouterFileURL("/v1/files/image", "abc123")
	if !strings.HasPrefix(signed, "/v1/files/image?") {
		t.Fatalf("signed URL missing route prefix: %s", signed)
	}
	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("signed URL parse error: %v", err)
	}
	if got := parsed.Query().Get("id"); got != "abc123" {
		t.Fatalf("signed URL id=%q, want abc123", got)
	}
	expRaw := parsed.Query().Get("exp")
	if expRaw == "" {
		t.Fatalf("signed URL missing exp")
	}
	exp, _ := strconv.ParseInt(expRaw, 10, 64)
	if exp != 1700000000+3600 {
		t.Fatalf("signed URL exp=%d, want %d", exp, 1700000000+3600)
	}
	if sig := parsed.Query().Get("sig"); sig == "" {
		t.Fatalf("signed URL missing sig")
	}
}

func TestRouterMediaSignatureValidAcceptsGoodAndRejectsBad(t *testing.T) {
	routerMediaSigningSecret = func() string { return "k" }
	t.Cleanup(func() { routerMediaSigningSecret = defaultRouterMediaSigningSecret })

	sig := routerMediaSignature("file1", 100)
	if !routerMediaSignatureValid("file1", 100, sig) {
		t.Fatalf("valid signature rejected")
	}
	if routerMediaSignatureValid("file1", 100, "deadbeef") {
		t.Fatalf("bad signature accepted")
	}
	if routerMediaSignatureValid("file2", 100, sig) {
		t.Fatalf("wrong file ID accepted")
	}
	if routerMediaSignatureValid("file1", 101, sig) {
		t.Fatalf("wrong expiry accepted")
	}
}

func TestRouterMediaTTLDefaultsToHour(t *testing.T) {
	if got := routerMediaTTL(); got != time.Hour {
		t.Fatalf("default TTL = %v, want 1h", got)
	}
}
