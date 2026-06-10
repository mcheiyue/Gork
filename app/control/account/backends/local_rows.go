package backends

import (
	"database/sql"
	"encoding/json"
	"strings"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

const localAccountColumns = `
token, pool, status, created_at, updated_at,
tags, quota_auto, quota_fast, quota_expert, quota_heavy, quota_grok_4_3, quota_console,
usage_use_count, usage_fail_count, usage_sync_count,
last_use_at, last_fail_at, last_fail_reason, last_sync_at, last_clear_at,
state_reason, deleted_at, ext, revision`

type localAccountRow struct {
	Token          string
	Pool           string
	Status         string
	CreatedAt      int64
	UpdatedAt      int64
	Tags           string
	QuotaAuto      string
	QuotaFast      string
	QuotaExpert    string
	QuotaHeavy     string
	QuotaGrok43    string
	QuotaConsole   string
	UsageUseCount  int
	UsageFailCount int
	UsageSyncCount int
	LastUseAt      sql.NullInt64
	LastFailAt     sql.NullInt64
	LastFailReason sql.NullString
	LastSyncAt     sql.NullInt64
	LastClearAt    sql.NullInt64
	StateReason    sql.NullString
	DeletedAt      sql.NullInt64
	Ext            string
	Revision       int
}

type localRowScanner interface {
	Scan(...any) error
}

func scanLocalAccount(scanner localRowScanner) (account.AccountRecord, error) {
	var row localAccountRow
	err := scanner.Scan(
		&row.Token, &row.Pool, &row.Status, &row.CreatedAt, &row.UpdatedAt,
		&row.Tags, &row.QuotaAuto, &row.QuotaFast, &row.QuotaExpert,
		&row.QuotaHeavy, &row.QuotaGrok43, &row.QuotaConsole,
		&row.UsageUseCount, &row.UsageFailCount, &row.UsageSyncCount,
		&row.LastUseAt, &row.LastFailAt, &row.LastFailReason,
		&row.LastSyncAt, &row.LastClearAt, &row.StateReason,
		&row.DeletedAt, &row.Ext, &row.Revision,
	)
	if err != nil {
		return account.AccountRecord{}, err
	}
	return row.toRecord()
}

func (row localAccountRow) toRecord() (account.AccountRecord, error) {
	tags, err := stringSliceFromJSON(row.Tags)
	if err != nil {
		return account.AccountRecord{}, err
	}
	quota, err := row.quotaMap()
	if err != nil {
		return account.AccountRecord{}, err
	}
	ext, err := anyMapFromJSON(row.Ext)
	if err != nil {
		return account.AccountRecord{}, err
	}
	return account.NewAccountRecord(account.AccountRecord{
		Token:          row.Token,
		Pool:           row.Pool,
		Status:         account.AccountStatus(row.Status),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		Tags:           tags,
		Quota:          quota,
		UsageUseCount:  row.UsageUseCount,
		UsageFailCount: row.UsageFailCount,
		UsageSyncCount: row.UsageSyncCount,
		LastUseAt:      nullableInt64(row.LastUseAt),
		LastFailAt:     nullableInt64(row.LastFailAt),
		LastFailReason: nullableString(row.LastFailReason),
		LastSyncAt:     nullableInt64(row.LastSyncAt),
		LastClearAt:    nullableInt64(row.LastClearAt),
		StateReason:    nullableString(row.StateReason),
		DeletedAt:      nullableInt64(row.DeletedAt),
		Ext:            ext,
		Revision:       row.Revision,
	})
}

func (row localAccountRow) quotaMap() (map[string]any, error) {
	quota := map[string]any{}
	for _, item := range []struct {
		name     string
		raw      string
		required bool
	}{
		{"auto", row.QuotaAuto, true},
		{"fast", row.QuotaFast, true},
		{"expert", row.QuotaExpert, true},
		{"heavy", row.QuotaHeavy, false},
		{"grok_4_3", row.QuotaGrok43, false},
		{"console", row.QuotaConsole, false},
	} {
		data, err := anyMapFromJSON(item.raw)
		if err != nil {
			return nil, err
		}
		if item.required || len(data) > 0 {
			quota[item.name] = data
		}
	}
	return quota, nil
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	out := value.Int64
	return &out
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

func anyMapFromJSON(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func stringSliceFromJSON(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "[]"
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

func jsonString(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
