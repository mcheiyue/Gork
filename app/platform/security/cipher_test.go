package security

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestCipherRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := c.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "refresh-secret" || !IsEncrypted(encrypted) {
		t.Fatalf("encrypted=%q", encrypted)
	}
	plain, err := c.Decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "refresh-secret" {
		t.Fatalf("plain=%q", plain)
	}
}

func TestCipherPlaintextCompatibleRead(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	c, err := NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.Decrypt("sso=legacy-plain-token")
	if err != nil {
		t.Fatal(err)
	}
	if plain != "sso=legacy-plain-token" {
		t.Fatalf("plain=%q", plain)
	}
}

func TestOpenCipherDisabled(t *testing.T) {
	c, err := OpenCipher("")
	if err != nil || c != nil {
		t.Fatalf("OpenCipher empty = %#v err=%v", c, err)
	}
	out, err := SealOptional(nil, "tok")
	if err != nil || out != "tok" {
		t.Fatalf("SealOptional nil = %q err=%v", out, err)
	}
	out, err = OpenOptional(nil, "tok")
	if err != nil || out != "tok" {
		t.Fatalf("OpenOptional nil = %q err=%v", out, err)
	}
	if _, err := OpenOptional(nil, encryptedPrefix+"dead"); err == nil {
		t.Fatal("expected error when encrypted value without key")
	}
}

func TestNewCipherRejectsBadKey(t *testing.T) {
	if _, err := NewCipher("not-base64!!!"); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := NewCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected length error")
	}
}

func TestEncryptEmpty(t *testing.T) {
	key := make([]byte, 32)
	c, err := NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Encrypt("")
	if err != nil || out != "" {
		t.Fatalf("empty encrypt = %q err=%v", out, err)
	}
}
