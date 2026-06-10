package account

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

var accountRecordNowMS = appruntime.NowMS

type AccountRecord struct {
	Token          string         `json:"token"`
	Pool           string         `json:"pool"`
	Status         AccountStatus  `json:"status"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
	Tags           []string       `json:"tags"`
	Quota          map[string]any `json:"quota"`
	UsageUseCount  int            `json:"usage_use_count"`
	UsageFailCount int            `json:"usage_fail_count"`
	UsageSyncCount int            `json:"usage_sync_count"`
	LastUseAt      *int64         `json:"last_use_at"`
	LastFailAt     *int64         `json:"last_fail_at"`
	LastFailReason *string        `json:"last_fail_reason"`
	LastSyncAt     *int64         `json:"last_sync_at"`
	LastClearAt    *int64         `json:"last_clear_at"`
	StateReason    *string        `json:"state_reason"`
	DeletedAt      *int64         `json:"deleted_at"`
	Ext            map[string]any `json:"ext"`
	Revision       int            `json:"revision"`
}

func NewAccountRecord(input AccountRecord) (AccountRecord, error) {
	token, err := NormalizeAccountToken(input.Token)
	if err != nil {
		return AccountRecord{}, err
	}
	pool, err := normalizeRecordPool(input.Pool)
	if err != nil {
		return AccountRecord{}, err
	}
	status, err := normalizeRecordStatus(input.Status)
	if err != nil {
		return AccountRecord{}, err
	}
	input.Token, input.Pool, input.Status = token, pool, status
	input.CreatedAt = defaultTimestamp(input.CreatedAt)
	input.UpdatedAt = defaultTimestamp(input.UpdatedAt)
	input.Tags = NormalizeAccountTags(input.Tags)
	input.Quota = cloneAnyMap(input.Quota)
	input.Ext = cloneAnyMap(input.Ext)
	return input, nil
}

func (r AccountRecord) IsNSFW() bool {
	for _, tag := range r.Tags {
		if tag == "nsfw" {
			return true
		}
	}
	return false
}

func (r AccountRecord) IsSuper() bool {
	return r.Pool == "super"
}

func (r AccountRecord) IsHeavy() bool {
	return r.Pool == "heavy"
}

func (r AccountRecord) IsDeleted() bool {
	return r.DeletedAt != nil
}

func (r AccountRecord) QuotaSet() (AccountQuotaSet, error) {
	return AccountQuotaSetFromDict(r.Quota)
}

func (r AccountRecord) WithQuotaSet(quotaSet AccountQuotaSet) AccountRecord {
	copied := r
	copied.Quota = quotaSet.ToDict()
	return copied
}

func NormalizeAccountToken(value any) (string, error) {
	if value == nil {
		return "", errors.New("token cannot be None")
	}
	token := normalizeTokenText(fmt.Sprint(value))
	if strings.HasPrefix(token, "sso=") {
		token = token[4:]
	}
	token = asciiOnly(token)
	if token == "" {
		return "", errors.New("token is empty after normalisation")
	}
	return token, nil
}

func NormalizeAccountPool(value any) (string, error) {
	val := ""
	if value != nil {
		val = fmt.Sprint(value)
	}
	val = strings.ToLower(strings.TrimSpace(val))
	if strings.Contains("super", val) {
		return "super", nil
	}
	if val == "heavy" {
		return "heavy", nil
	}
	if val == "ssobasic" || val == "basic" || val == "" || val == "auto" {
		return "basic", nil
	}
	return "", fmt.Errorf("unknown pool: %v", value)
}

func NormalizeAccountTags(value any) []string {
	switch v := value.(type) {
	case nil:
		return []string{}
	case string:
		return uniqueTags(strings.Split(v, ","))
	case []string:
		return uniqueTags(v)
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, fmt.Sprint(item))
		}
		return uniqueTags(items)
	default:
		return uniqueTags([]string{fmt.Sprint(value)})
	}
}

func normalizeTokenText(value string) string {
	var b strings.Builder
	for _, r := range value {
		mapped, keep := normalizeTokenRune(r)
		if !keep || unicode.IsSpace(mapped) {
			continue
		}
		b.WriteRune(mapped)
	}
	return b.String()
}

func normalizeTokenRune(r rune) (rune, bool) {
	switch r {
	case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2212':
		return '-', true
	case '\u00a0', '\u2007', '\u202f':
		return ' ', true
	case '\u200b', '\u200c', '\u200d', '\ufeff':
		return 0, false
	default:
		return r, true
	}
}

func asciiOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r <= 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeRecordPool(value string) (string, error) {
	if value == "" {
		return "basic", nil
	}
	return NormalizeAccountPool(value)
}

func normalizeRecordStatus(value AccountStatus) (AccountStatus, error) {
	if value == "" {
		return AccountStatusActive, nil
	}
	if _, ok := ParseAccountStatus(value.String()); !ok {
		return "", fmt.Errorf("unknown account status: %s", value)
	}
	return value, nil
}

func defaultTimestamp(value int64) int64 {
	if value != 0 {
		return value
	}
	return accountRecordNowMS()
}

func uniqueTags(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		tag := strings.TrimSpace(value)
		if tag != "" && !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

func cloneAnyMap(value map[string]any) map[string]any {
	out := map[string]any{}
	for key, item := range value {
		out[key] = item
	}
	return out
}
