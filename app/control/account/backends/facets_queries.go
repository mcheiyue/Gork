package backends

import (
	"context"
	"database/sql"

	account "github.com/jiujiu532/grok2api/app/control/account"
)

const accountFacetColumns = `
pool, status, tags, quota_auto, quota_fast, quota_expert, quota_heavy, quota_console,
usage_use_count, usage_fail_count`

type accountFacetRow struct {
	Pool           string
	Status         string
	Tags           string
	QuotaAuto      string
	QuotaFast      string
	QuotaExpert    string
	QuotaHeavy     string
	QuotaConsole   string
	UsageUseCount  int
	UsageFailCount int
}

func (r *LocalAccountRepository) ListFacets(ctx context.Context) (account.AccountFacetSnapshot, error) {
	r.lock.Lock()
	defer r.lock.Unlock()
	db, err := r.connect(ctx)
	if err != nil {
		return account.AccountFacetSnapshot{}, err
	}
	defer db.Close()
	return listAccountFacets(ctx, db, localAccountTable)
}

func (r *SQLAccountRepository) ListFacets(ctx context.Context) (account.AccountFacetSnapshot, error) {
	if err := r.ensureInitialized(ctx); err != nil {
		return account.AccountFacetSnapshot{}, err
	}
	return listAccountFacets(ctx, r.db, "accounts")
}

func listAccountFacets(ctx context.Context, db localSQLRunner, table string) (account.AccountFacetSnapshot, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+accountFacetColumns+" FROM "+table+" WHERE deleted_at IS NULL")
	if err != nil {
		return account.AccountFacetSnapshot{}, err
	}
	defer rows.Close()
	return scanAccountFacetRows(rows)
}

func scanAccountFacetRows(rows *sql.Rows) (account.AccountFacetSnapshot, error) {
	snapshot := account.NewAccountFacetSnapshot()
	for rows.Next() {
		var row accountFacetRow
		if err := rows.Scan(
			&row.Pool,
			&row.Status,
			&row.Tags,
			&row.QuotaAuto,
			&row.QuotaFast,
			&row.QuotaExpert,
			&row.QuotaHeavy,
			&row.QuotaConsole,
			&row.UsageUseCount,
			&row.UsageFailCount,
		); err != nil {
			return account.AccountFacetSnapshot{}, err
		}
		if err := applyAccountFacetRow(&snapshot, row); err != nil {
			return account.AccountFacetSnapshot{}, err
		}
	}
	return snapshot, rows.Err()
}

func applyAccountFacetRow(snapshot *account.AccountFacetSnapshot, row accountFacetRow) error {
	pool := row.Pool
	if pool == "" {
		pool = "basic"
	}
	snapshot.Pools["all"]++
	snapshot.Pools[pool]++

	status := row.Status
	snapshot.Status["all"]++
	switch status {
	case "active", "cooling", "disabled":
		snapshot.Status[status]++
		snapshot.Stats[status]++
	default:
		snapshot.Status["invalid"]++
		snapshot.Stats["invalid"]++
	}

	tags, err := stringSliceFromJSON(row.Tags)
	if err != nil {
		return err
	}
	snapshot.NSFW["all"]++
	if facetHasString(tags, "nsfw") {
		snapshot.NSFW["enabled"]++
	} else {
		snapshot.NSFW["disabled"]++
	}

	snapshot.Stats["success"] += row.UsageUseCount
	snapshot.Stats["fail"] += row.UsageFailCount
	snapshot.Stats["calls"] += row.UsageUseCount + row.UsageFailCount
	if err := addFacetQuotaRemaining(snapshot, "qa", row.QuotaAuto); err != nil {
		return err
	}
	if err := addFacetQuotaRemaining(snapshot, "qf", row.QuotaFast); err != nil {
		return err
	}
	if err := addFacetQuotaRemaining(snapshot, "qe", row.QuotaExpert); err != nil {
		return err
	}
	if err := addFacetQuotaRemaining(snapshot, "qh", row.QuotaHeavy); err != nil {
		return err
	}
	if err := addFacetQuotaRemaining(snapshot, "qc", row.QuotaConsole); err != nil {
		return err
	}
	return nil
}

func addFacetQuotaRemaining(snapshot *account.AccountFacetSnapshot, key string, raw string) error {
	quota, err := anyMapFromJSON(raw)
	if err != nil {
		return err
	}
	snapshot.Stats[key] += anyInt(quota["remaining"])
	return nil
}

func facetHasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func anyInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
