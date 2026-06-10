package account

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"
	AccountStatusCooling  AccountStatus = "cooling"
	AccountStatusExpired  AccountStatus = "expired"
	AccountStatusDisabled AccountStatus = "disabled"
)

func (s AccountStatus) String() string {
	return string(s)
}

func ParseAccountStatus(value string) (AccountStatus, bool) {
	switch AccountStatus(value) {
	case AccountStatusActive, AccountStatusCooling, AccountStatusExpired, AccountStatusDisabled:
		return AccountStatus(value), true
	default:
		return "", false
	}
}

func (s AccountStatus) MarshalJSON() ([]byte, error) {
	if _, ok := ParseAccountStatus(s.String()); !ok {
		return nil, fmt.Errorf("unknown account status %q", s)
	}
	return json.Marshal(s.String())
}

func (s *AccountStatus) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, ok := ParseAccountStatus(value)
	if !ok {
		return fmt.Errorf("unknown account status %q", value)
	}
	*s = parsed
	return nil
}

type QuotaSource int

const (
	QuotaSourceDefault QuotaSource = iota
	QuotaSourceReal
	QuotaSourceEstimated
)

func (s QuotaSource) String() string {
	return strconv.Itoa(int(s))
}

func ParseQuotaSource(value int) (QuotaSource, bool) {
	switch QuotaSource(value) {
	case QuotaSourceDefault, QuotaSourceReal, QuotaSourceEstimated:
		return QuotaSource(value), true
	default:
		return QuotaSourceDefault, false
	}
}

func (s QuotaSource) MarshalJSON() ([]byte, error) {
	if _, ok := ParseQuotaSource(int(s)); !ok {
		return nil, fmt.Errorf("unknown quota source %d", s)
	}
	return json.Marshal(int(s))
}

func (s *QuotaSource) UnmarshalJSON(data []byte) error {
	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, ok := ParseQuotaSource(value)
	if !ok {
		return fmt.Errorf("unknown quota source %d", value)
	}
	*s = parsed
	return nil
}

type FeedbackKind string

const (
	FeedbackKindSuccess      FeedbackKind = "success"
	FeedbackKindUnauthorized FeedbackKind = "unauthorized"
	FeedbackKindForbidden    FeedbackKind = "forbidden"
	FeedbackKindRateLimited  FeedbackKind = "rate_limited"
	FeedbackKindServerError  FeedbackKind = "server_error"
	FeedbackKindDisable      FeedbackKind = "disable"
	FeedbackKindDelete       FeedbackKind = "delete"
	FeedbackKindRestore      FeedbackKind = "restore"
)

func (k FeedbackKind) String() string {
	return string(k)
}

func ParseFeedbackKind(value string) (FeedbackKind, bool) {
	switch FeedbackKind(value) {
	case FeedbackKindSuccess, FeedbackKindUnauthorized, FeedbackKindForbidden,
		FeedbackKindRateLimited, FeedbackKindServerError, FeedbackKindDisable,
		FeedbackKindDelete, FeedbackKindRestore:
		return FeedbackKind(value), true
	default:
		return "", false
	}
}

func (k FeedbackKind) MarshalJSON() ([]byte, error) {
	if _, ok := ParseFeedbackKind(k.String()); !ok {
		return nil, fmt.Errorf("unknown feedback kind %q", k)
	}
	return json.Marshal(k.String())
}

func (k *FeedbackKind) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, ok := ParseFeedbackKind(value)
	if !ok {
		return fmt.Errorf("unknown feedback kind %q", value)
	}
	*k = parsed
	return nil
}
