package account

import appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"

const (
	successStep       = 0.12
	authFactor        = 0.55
	forbiddenFactor   = 0.25
	rateLimitFactor   = 0.45
	serverErrorFactor = 0.75
	minHealth         = 0.05
	maxHealth         = 1.0
)

func ApplySuccessQuota(table *AccountRuntimeTable, idx int, modeID int) {
	quotaCol := table.quotaCol(modeID)
	quotaCol[idx] = maxInt(0, quotaCol[idx]-1)
	bumpHealth(table, idx)
}

func ApplySuccessRandom(table *AccountRuntimeTable, idx int) {
	bumpHealth(table, idx)
}

func ApplyRateLimitedQuota(table *AccountRuntimeTable, idx int, modeID int) {
	table.quotaCol(modeID)[idx] = 0
	adjustHealth(table, idx, rateLimitFactor)
}

func ApplyRateLimitedRandom(table *AccountRuntimeTable, idx int, coolingSec int) {
	ts := int(appruntime.NowS()) + maxInt(0, coolingSec)
	if ts > table.CoolingUntilSByIdx[idx] {
		table.CoolingUntilSByIdx[idx] = ts
	}
	adjustHealth(table, idx, rateLimitFactor)
}

func ApplyAuthFailure(table *AccountRuntimeTable, idx int) {
	adjustHealth(table, idx, authFactor)
}

func ApplyForbidden(table *AccountRuntimeTable, idx int) {
	adjustHealth(table, idx, forbiddenFactor)
}

func ApplyServerError(table *AccountRuntimeTable, idx int) {
	adjustHealth(table, idx, serverErrorFactor)
}

func ApplyStatusChange(table *AccountRuntimeTable, idx int, newStatusID int) {
	poolID := table.PoolByIdx[idx]
	oldStatus := table.StatusByIdx[idx]
	if oldStatus == newStatusID {
		return
	}
	table.StatusByIdx[idx] = newStatusID
	if newStatusID != StatusActive {
		for _, modeID := range allModeIDs {
			key := ModeKey{PoolID: poolID, ModeID: modeID}
			if bucket := table.ModeAvailable[key]; bucket != nil {
				delete(bucket, idx)
			}
		}
		return
	}
	for _, modeID := range allModeIDs {
		if table.quotaCol(modeID)[idx] > 0 {
			key := ModeKey{PoolID: poolID, ModeID: modeID}
			if table.ModeAvailable[key] == nil {
				table.ModeAvailable[key] = map[int]bool{}
			}
			table.ModeAvailable[key][idx] = true
		}
	}
}

func ApplyQuotaUpdate(table *AccountRuntimeTable, idx int, modeID int, remaining int, resetS int) {
	table.quotaCol(modeID)[idx] = clampInt(remaining, 0, 32767)
	table.resetCol(modeID)[idx] = resetS
	poolID := table.PoolByIdx[idx]
	if table.StatusByIdx[idx] == StatusActive {
		key := ModeKey{PoolID: poolID, ModeID: modeID}
		if table.ModeAvailable[key] == nil {
			table.ModeAvailable[key] = map[int]bool{}
		}
		if table.windowCol(modeID)[idx] > 0 {
			table.ModeAvailable[key][idx] = true
		}
	}
}

func IncrementInflight(table *AccountRuntimeTable, idx int) {
	table.InflightByIdx[idx] = minInt(table.InflightByIdx[idx]+1, 65535)
}

func DecrementInflight(table *AccountRuntimeTable, idx int) {
	table.InflightByIdx[idx] = maxInt(0, table.InflightByIdx[idx]-1)
}

func UpdateLastUse(table *AccountRuntimeTable, idx int, nowS int) {
	table.LastUseAtByIdx[idx] = nowS
}

func UpdateLastFail(table *AccountRuntimeTable, idx int, nowS int) {
	table.LastFailAtByIdx[idx] = nowS
	table.FailCountByIdx[idx] = minInt(table.FailCountByIdx[idx]+1, 65535)
}

func bumpHealth(table *AccountRuntimeTable, idx int) {
	table.HealthByIdx[idx] = minFloat(maxHealth, table.HealthByIdx[idx]+successStep)
}

func adjustHealth(table *AccountRuntimeTable, idx int, factor float64) {
	table.HealthByIdx[idx] = maxFloat(minHealth, table.HealthByIdx[idx]*factor)
}

func clampInt(value int, minimum int, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func minFloat(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
