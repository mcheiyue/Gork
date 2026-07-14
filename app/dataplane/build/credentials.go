package build

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const maxCredentialImportAccounts = 10000

type credentialImportDocument struct {
	Accounts []importedCredentialEntry `json:"accounts"`
}

type importedCredentialEntry struct {
	Provider     string `json:"provider"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresAt    string `json:"expires_at"`
	ExpiresIn    int64  `json:"expires_in"`
	Email        string `json:"email"`
	UserID       string `json:"user_id"`
	PrincipalID  string `json:"principal_id"`
	TeamID       string `json:"team_id"`
}

// ParseCredentials 宽松解析 chenyme 兼容的 Build 凭据 JSON。
// 支持 {"accounts":[...]} 批量与单对象两种形态。
func ParseCredentials(data []byte) ([]Credential, error) {
	var shape map[string]json.RawMessage
	if err := json.Unmarshal(data, &shape); err != nil {
		return nil, fmt.Errorf("解析账号凭据 JSON: %w", err)
	}

	var entries []importedCredentialEntry
	if _, batch := shape["accounts"]; batch {
		var document credentialImportDocument
		if err := json.Unmarshal(data, &document); err != nil {
			return nil, fmt.Errorf("解析批量账号凭据: %w", err)
		}
		entries = document.Accounts
	} else {
		var entry importedCredentialEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("解析 OAuth 凭据: %w", err)
		}
		entries = []importedCredentialEntry{entry}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("账号凭据中没有账号")
	}
	if len(entries) > maxCredentialImportAccounts {
		return nil, fmt.Errorf("单次最多导入 %d 个账号", maxCredentialImportAccounts)
	}

	result := make([]Credential, 0, len(entries))
	for index, entry := range entries {
		cred, err := normalizeImportedCredential(entry)
		if err != nil {
			return nil, fmt.Errorf("第 %d 个账号: %w", index+1, err)
		}
		result = append(result, cred)
	}
	return result, nil
}

// MarshalCredentials 导出为 chenyme 兼容 JSON（不含密钥日志，仅结构化序列化）。
func MarshalCredentials(values []Credential) ([]byte, error) {
	document := credentialImportDocument{Accounts: make([]importedCredentialEntry, 0, len(values))}
	for _, value := range values {
		entry := importedCredentialEntry{
			Provider:     firstNonEmpty(value.Provider, CredentialProvider),
			Name:         value.Name,
			ClientID:     value.ClientID,
			AccessToken:  value.AccessToken,
			RefreshToken: value.RefreshToken,
			TokenType:    "Bearer",
			Email:        value.Email,
			UserID:       value.UserID,
			TeamID:       value.TeamID,
		}
		if !value.ExpiresAt.IsZero() {
			entry.ExpiresAt = value.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		document.Accounts = append(document.Accounts, entry)
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化账号凭据: %w", err)
	}
	return append(data, '\n'), nil
}

func normalizeImportedCredential(entry importedCredentialEntry) (Credential, error) {
	providerName := strings.ToLower(strings.TrimSpace(entry.Provider))
	if providerName == "" {
		providerName = CredentialProvider
	}
	if providerName != CredentialProvider {
		return Credential{}, fmt.Errorf("暂不支持 Provider %q", entry.Provider)
	}
	accessToken := strings.TrimSpace(entry.AccessToken)
	refreshToken := strings.TrimSpace(entry.RefreshToken)
	if accessToken == "" && refreshToken == "" {
		return Credential{}, fmt.Errorf("access_token 和 refresh_token 至少提供一个")
	}
	if entry.TokenType != "" && !strings.EqualFold(strings.TrimSpace(entry.TokenType), "Bearer") {
		return Credential{}, fmt.Errorf("暂不支持 token_type %q", entry.TokenType)
	}

	claims := decodeJWTClaims(firstNonEmpty(entry.IDToken, accessToken))
	userID := firstNonEmpty(entry.UserID, entry.PrincipalID, stringClaim(claims, "sub"))
	email := firstNonEmpty(entry.Email, stringClaim(claims, "email"))
	teamID := firstNonEmpty(entry.TeamID, stringClaim(claims, "team_id"))
	expiresAt, err := importedCredentialExpiry(entry, claims)
	if err != nil {
		return Credential{}, err
	}

	name := strings.TrimSpace(entry.Name)
	if name == "" {
		name = firstNonEmpty(email, userID, "build-account")
	}

	return Credential{
		Provider:     providerName,
		Name:         name,
		ClientID:     firstNonEmpty(entry.ClientID, DefaultOAuthClientID),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IDToken:      strings.TrimSpace(entry.IDToken),
		ExpiresAt:    expiresAt,
		Email:        email,
		UserID:       userID,
		TeamID:       teamID,
	}, nil
}

func importedCredentialExpiry(entry importedCredentialEntry, claims map[string]any) (time.Time, error) {
	if strings.TrimSpace(entry.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, entry.ExpiresAt)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339, entry.ExpiresAt)
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("expires_at 无效: %w", err)
		}
		return parsed.UTC(), nil
	}
	if entry.ExpiresIn > 0 {
		return time.Now().UTC().Add(time.Duration(entry.ExpiresIn) * time.Second), nil
	}
	if exp, ok := claims["exp"].(float64); ok && exp > 0 {
		return time.Unix(int64(exp), 0).UTC(), nil
	}
	return time.Time{}, nil
}

// decodeJWTClaims 仅解 payload，不验签（与 chenyme 一致，仅用于展示字段）。
func decodeJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// 兼容标准 padding
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func stringClaim(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	v, ok := claims[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

// CredentialFromToken 将 OAuth TokenPayload 转为可落库 Credential。
func CredentialFromToken(name string, payload TokenPayload, clientID string) Credential {
	claims := decodeJWTClaims(firstNonEmpty(payload.IDToken, payload.AccessToken))
	email := stringClaim(claims, "email")
	userID := stringClaim(claims, "sub")
	if name == "" {
		name = firstNonEmpty(email, userID, "build-account")
	}
	return Credential{
		Provider:     CredentialProvider,
		Name:         name,
		ClientID:     firstNonEmpty(clientID, DefaultOAuthClientID),
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		IDToken:      payload.IDToken,
		ExpiresAt:    payload.ExpiresAt,
		Email:        email,
		UserID:       userID,
		TeamID:       stringClaim(claims, "team_id"),
	}
}
