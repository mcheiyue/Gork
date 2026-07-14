package build

import (
	"errors"
	"fmt"
	"time"
)

// 与 RFC 8628 / xAI OAuth 错误码对齐的哨兵错误。
var (
	ErrAuthorizationPending = errors.New("build oauth: authorization_pending")
	ErrSlowDown             = errors.New("build oauth: slow_down")
	ErrAuthorizationDenied  = errors.New("build oauth: access_denied")
)

// RefreshError 描述 token 交换/刷新失败；Permanent 表示账号应禁用。
type RefreshError struct {
	Status     int
	Code       string
	Permanent  bool
	RetryAfter time.Duration
	Cause      error
}

func (e *RefreshError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("build oauth refresh %s: %v", e.Code, e.Cause)
	}
	if e.Code != "" {
		return fmt.Sprintf("build oauth refresh: %s", e.Code)
	}
	return "build oauth refresh failed"
}

func (e *RefreshError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsPermanentRefresh 判断错误是否为永久刷新失败。
func IsPermanentRefresh(err error) bool {
	var re *RefreshError
	if errors.As(err, &re) {
		return re.Permanent
	}
	return errors.Is(err, ErrAuthorizationDenied)
}

// IsAuthorizationPending 用户尚未在浏览器完成 Device 授权。
func IsAuthorizationPending(err error) bool {
	return errors.Is(err, ErrAuthorizationPending)
}

// IsSlowDown 轮询过快，应加大 interval。
func IsSlowDown(err error) bool {
	return errors.Is(err, ErrSlowDown)
}
