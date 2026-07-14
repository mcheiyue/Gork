package buildaccount

import (
	"time"

	"github.com/dslzl/gork/app/dataplane/build"
)

// 账号状态。
const (
	StatusActive   = "active"
	StatusCooling  = "cooling"
	StatusDisabled = "disabled"
	StatusExpired  = "expired"
)

// Account 是 Build 账号的运行时视图（token 已解密为明文）。
type Account struct {
	ID           int64
	Name         string
	Email        string
	UserID       string
	ClientID     string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Status       string
	CoolingUntil time.Time
	Priority     int
	LastUseAt    time.Time
	FailCount    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Credential 转为可写入记录。
func FromCredential(cred build.Credential) Account {
	status := StatusActive
	if cred.AccessToken == "" && cred.RefreshToken == "" {
		status = StatusDisabled
	}
	return Account{
		Name:         cred.Name,
		Email:        cred.Email,
		UserID:       cred.UserID,
		ClientID:     cred.ClientID,
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		ExpiresAt:    cred.ExpiresAt,
		Status:       status,
		Priority:     0,
	}
}

// ToCredential 导出为 dataplane Credential。
func (a Account) ToCredential() build.Credential {
	return build.Credential{
		Provider:     build.CredentialProvider,
		Name:         a.Name,
		ClientID:     a.ClientID,
		AccessToken:  a.AccessToken,
		RefreshToken: a.RefreshToken,
		ExpiresAt:    a.ExpiresAt,
		Email:        a.Email,
		UserID:       a.UserID,
	}
}

// NeedsRefresh 判断 access token 是否即将过期（默认 120s 余量）。
func (a Account) NeedsRefresh(now time.Time, skew time.Duration) bool {
	if a.AccessToken == "" {
		return a.RefreshToken != ""
	}
	if a.ExpiresAt.IsZero() {
		return false
	}
	if skew <= 0 {
		skew = 2 * time.Minute
	}
	return !a.ExpiresAt.After(now.Add(skew))
}
