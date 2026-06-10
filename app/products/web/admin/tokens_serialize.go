package admin

func adminTokenSerialize(account adminAssetsAccount) map[string]any {
	return map[string]any{
		"token": account.Token, "pool": adminTokenPool(account.Pool), "status": account.Status,
		"quota": adminQuotaBrief(account.Quota), "use_count": account.UsageUseCount,
		"last_used_at": account.LastUseAt, "tags": account.Tags,
	}
}

func adminQuotaBrief(quota map[string]any) map[string]any {
	out := map[string]any{}
	for _, mode := range []string{"auto", "fast", "expert", "heavy", "console"} {
		if raw, ok := quota[mode]; ok {
			if value, ok := raw.(map[string]any); ok {
				out[mode] = map[string]any{"remaining": adminAnyInt(value["remaining"]), "total": adminAnyInt(value["total"])}
			}
		}
	}
	return out
}

func adminTokensFacetSnapshotFromRecords(records []adminAssetsAccount) adminTokensFacetSnapshot {
	status := map[string]int{"all": 0, "active": 0, "cooling": 0, "invalid": 0, "disabled": 0}
	nsfw := map[string]int{"all": 0, "enabled": 0, "disabled": 0}
	pools := map[string]int{"all": 0, "basic": 0, "super": 0, "heavy": 0}
	stats := map[string]int{"active": 0, "cooling": 0, "invalid": 0, "disabled": 0, "calls": 0, "success": 0, "fail": 0, "qa": 0, "qf": 0, "qe": 0, "qh": 0, "qc": 0}
	for _, record := range records {
		adminTokensFacetRecord(record, status, nsfw, pools, stats)
	}
	return adminTokensFacetSnapshot{Status: status, NSFW: nsfw, Pools: pools, Stats: stats}
}

func (s adminTokensFacetSnapshot) ToMap() map[string]any {
	return map[string]any{"status": s.Status, "nsfw": s.NSFW, "pools": s.Pools, "stats": s.Stats}
}

func adminTokensFacetRecord(record adminAssetsAccount, status map[string]int, nsfw map[string]int, pools map[string]int, stats map[string]int) {
	pool := adminTokenPool(record.Pool)
	pools["all"]++
	pools[pool]++
	state := record.Status
	status["all"]++
	switch state {
	case "active", "cooling", "disabled":
		status[state]++
		stats[state]++
	default:
		status["invalid"]++
		stats["invalid"]++
	}
	enabled := adminTokenHasTag(record.Tags, "nsfw")
	nsfw["all"]++
	if enabled {
		nsfw["enabled"]++
	} else {
		nsfw["disabled"]++
	}
	adminTokensFacetUsage(record, stats)
}

func adminTokensFacetUsage(record adminAssetsAccount, stats map[string]int) {
	stats["success"] += record.UsageUseCount
	stats["fail"] += record.UsageFailCount
	stats["calls"] += record.UsageUseCount + record.UsageFailCount
	stats["qa"] += adminQuotaRemaining(record.Quota, "auto")
	stats["qf"] += adminQuotaRemaining(record.Quota, "fast")
	stats["qe"] += adminQuotaRemaining(record.Quota, "expert")
	stats["qh"] += adminQuotaRemaining(record.Quota, "heavy")
	stats["qc"] += adminQuotaRemaining(record.Quota, "console")
}

func adminQuotaRemaining(quota map[string]any, mode string) int {
	if value, ok := quota[mode].(map[string]any); ok {
		return adminAnyInt(value["remaining"])
	}
	return 0
}

func adminTokenHasTag(tags []string, target string) bool {
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}

func adminAnyInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case nil:
		return 0
	default:
		return 0
	}
}
