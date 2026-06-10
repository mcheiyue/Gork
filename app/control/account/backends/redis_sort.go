package backends

import (
	"sort"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

func sortRedisRecords(records []account.AccountRecord, sortBy string, desc bool) {
	sort.Slice(records, func(i, j int) bool {
		left := redisSortValue(records[i], sortBy)
		right := redisSortValue(records[j], sortBy)
		if left == right {
			if desc {
				return records[i].Token > records[j].Token
			}
			return records[i].Token < records[j].Token
		}
		if desc {
			return left > right
		}
		return left < right
	})
}

func redisSortValue(record account.AccountRecord, field string) int {
	switch field {
	case "created_at":
		return int(record.CreatedAt)
	case "updated_at":
		return int(record.UpdatedAt)
	case "last_use_at":
		return intFromPtr(record.LastUseAt)
	case "last_fail_at":
		return intFromPtr(record.LastFailAt)
	case "last_sync_at":
		return intFromPtr(record.LastSyncAt)
	case "last_clear_at":
		return intFromPtr(record.LastClearAt)
	case "usage_use_count":
		return record.UsageUseCount
	case "usage_fail_count":
		return record.UsageFailCount
	case "usage_sync_count":
		return record.UsageSyncCount
	default:
		return 0
	}
}

func intFromPtr(value *int64) int {
	if value == nil {
		return 0
	}
	return int(*value)
}
