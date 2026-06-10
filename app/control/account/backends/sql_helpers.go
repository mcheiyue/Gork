package backends

import (
	"strings"
)

func sqlPlaceholders(dialect SQLDialect, start int, count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = sqlBind(dialect, start+i)
	}
	return strings.Join(items, ",")
}

func sqlScanChangesQuery(dialect SQLDialect) string {
	return "SELECT " + localAccountColumns + " FROM accounts WHERE revision > " +
		sqlBind(dialect, 1) + " ORDER BY revision LIMIT " + sqlBind(dialect, 2)
}

func safeSQLSort(sortBy string) string {
	switch sortBy {
	case "token", "pool", "status", "created_at", "updated_at", "tags",
		"usage_use_count", "usage_fail_count", "usage_sync_count",
		"last_use_at", "last_fail_at", "last_sync_at", "last_clear_at",
		"state_reason", "deleted_at", "revision":
		return sortBy
	default:
		return "updated_at"
	}
}

func sqlAssignments(dialect SQLDialect, sets []localPatchSet) (string, []any) {
	assignments := make([]string, 0, len(sets))
	values := make([]any, 0, len(sets))
	for _, set := range dedupeSQLPatchSets(sets) {
		values = append(values, set.value)
		assignments = append(assignments, set.column+" = "+sqlBind(dialect, len(values)))
	}
	return strings.Join(assignments, ", "), values
}

func dedupeSQLPatchSets(sets []localPatchSet) []localPatchSet {
	seen := map[string]bool{}
	reversed := make([]localPatchSet, 0, len(sets))
	for i := len(sets) - 1; i >= 0; i-- {
		if seen[sets[i].column] {
			continue
		}
		seen[sets[i].column] = true
		reversed = append(reversed, sets[i])
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}
