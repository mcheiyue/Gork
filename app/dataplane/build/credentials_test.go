package build

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseCredentialsBatchAndSingle(t *testing.T) {
	batch := []byte(`{
  "accounts": [
    {
      "provider": "grok_build",
      "name": "a1",
      "access_token": "at-1",
      "refresh_token": "rt-1",
      "expires_at": "2030-01-02T03:04:05Z",
      "email": "a@example.com",
      "user_id": "u1"
    }
  ]
}`)
	creds, err := ParseCredentials(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Email != "a@example.com" || creds[0].RefreshToken != "rt-1" {
		t.Fatalf("creds=%#v", creds)
	}

	single := []byte(`{"access_token":"at-only","provider":"grok_build"}`)
	creds, err = ParseCredentials(single)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].AccessToken != "at-only" || creds[0].Name == "" {
		t.Fatalf("single=%#v", creds)
	}
}

func TestParseCredentialsRejectsForeignProvider(t *testing.T) {
	_, err := ParseCredentials([]byte(`{"provider":"other","access_token":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseCredentialsJWTClaims(t *testing.T) {
	// {"sub":"user-42","email":"jwt@example.com"}
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user-42","email":"jwt@example.com"}`))
	token := "hdr." + payload + ".sig"
	creds, err := ParseCredentials([]byte(`{"access_token":"` + token + `","provider":"grok_build"}`))
	if err != nil {
		t.Fatal(err)
	}
	if creds[0].UserID != "user-42" || creds[0].Email != "jwt@example.com" {
		t.Fatalf("creds=%#v", creds[0])
	}
}

func TestMarshalCredentialsRoundTrip(t *testing.T) {
	in := []Credential{{
		Provider:     CredentialProvider,
		Name:         "n1",
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		Email:        "e@x.com",
		UserID:       "u",
	}}
	raw, err := MarshalCredentials(in)
	if err != nil {
		t.Fatal(err)
	}
	// 确保序列化里没有把测试 token 以外的真实密钥模式；此处仅为假数据
	if !strings.Contains(string(raw), "access_token") {
		t.Fatalf("raw=%s", raw)
	}
	out, err := ParseCredentials(raw)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].RefreshToken != "rt" || out[0].Email != "e@x.com" {
		t.Fatalf("out=%#v", out)
	}
}

func TestCredentialFromToken(t *testing.T) {
	payloadJSON, _ := json.Marshal(map[string]any{"sub": "s1", "email": "e1@x.com"})
	idToken := "x." + base64.RawURLEncoding.EncodeToString(payloadJSON) + ".y"
	cred := CredentialFromToken("", TokenPayload{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		IDToken:      idToken,
	}, "")
	if cred.Email != "e1@x.com" || cred.UserID != "s1" || cred.Provider != CredentialProvider {
		t.Fatalf("cred=%#v", cred)
	}
}
