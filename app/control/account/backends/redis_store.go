package backends

import "context"

type RedisAccountStore interface {
	Incr(context.Context, string) (int, error)
	SetNX(context.Context, string, string) (bool, error)
	Get(context.Context, string) (string, bool, error)
	ScanKeys(context.Context, string) ([]string, error)
	HGetAll(context.Context, string) (map[string]string, error)
	HGet(context.Context, string, string) (string, bool, error)
	HSet(context.Context, string, map[string]string) error
	ZAdd(context.Context, string, map[string]int) error
	ZRangeByScore(context.Context, string, int, int) ([]string, error)
	ZRem(context.Context, string, ...string) error
	SAdd(context.Context, string, ...string) error
	SRem(context.Context, string, ...string) error
	SMembers(context.Context, string) ([]string, error)
	Close(context.Context) error
}

const (
	redisKeyRevision     = "accounts:rev"
	redisKeyRevisionLog  = "accounts:revision_log"
	redisKeyIndexReady   = "accounts:index:ready"
	redisKeyIndexAll     = "accounts:index:all"
	redisKeyIndexLive    = "accounts:index:live"
	redisKeyIndexDeleted = "accounts:index:deleted"
)

var redisSortFields = []string{
	"created_at",
	"updated_at",
	"last_use_at",
	"last_fail_at",
	"last_sync_at",
	"last_clear_at",
	"usage_use_count",
	"usage_fail_count",
	"usage_sync_count",
}

func redisRecordKey(token string) string {
	return "accounts:record:" + token
}

func redisPoolKey(pool string) string {
	return "accounts:pool:" + pool
}

func redisIndexPoolKey(pool string) string {
	return "accounts:index:pool:" + pool
}

func redisIndexStatusKey(status string) string {
	return "accounts:index:status:" + status
}

func redisIndexTagKey(tag string) string {
	return "accounts:index:tag:" + tag
}

func redisSortKey(field string) string {
	return "accounts:sort:" + field
}
