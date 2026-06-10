package account

import (
	"fmt"
	"math/rand"
	"sync"
)

const (
	strategyQuota  = "quota"
	strategyRandom = "random"

	weightHealth   = 100.0
	weightQuota    = 25.0
	weightRecent   = 15.0
	weightInflight = 20.0
	weightFail     = 4.0
	recentWindowS  = 15

	defaultMaxInflight = 8
)

var selectorState = struct {
	sync.RWMutex
	strategy string
}{strategy: strategyRandom}

type SelectOptions struct {
	ExcludeIdxs   map[int]bool
	PreferTagIdxs map[int]bool
	NowS          int
	MaxInflight   int
}

func SetStrategy(name string) error {
	if name != strategyQuota && name != strategyRandom {
		return fmt.Errorf("unknown selection strategy: %q", name)
	}
	selectorState.Lock()
	defer selectorState.Unlock()
	selectorState.strategy = name
	return nil
}

func CurrentStrategy() string {
	selectorState.RLock()
	defer selectorState.RUnlock()
	return selectorState.strategy
}

func Select(table *AccountRuntimeTable, poolID int, modeID int, options SelectOptions) (int, bool) {
	if CurrentStrategy() == strategyRandom {
		return randomSelect(table, poolID, options)
	}
	return quotaSelect(table, poolID, modeID, options)
}

func SelectAny(table *AccountRuntimeTable, poolID int, options SelectOptions) (int, bool) {
	if CurrentStrategy() == strategyRandom {
		return randomSelect(table, poolID, options)
	}
	return quotaSelectAny(table, poolID, options)
}

func quotaSelect(table *AccountRuntimeTable, poolID int, modeID int, options SelectOptions) (int, bool) {
	candidates := table.ModeAvailable[ModeKey{PoolID: poolID, ModeID: modeID}]
	if len(candidates) == 0 {
		return 0, false
	}
	resetCol := table.resetCol(modeID)
	quotaCol := table.quotaCol(modeID)
	totalCol := table.totalCol(modeID)
	windowCol := table.windowCol(modeID)
	maybeResetWindows(table, candidates, modeID, resetCol, quotaCol, totalCol, windowCol, poolID, options.NowS)
	working := map[int]bool{}
	for idx := range candidates {
		if options.ExcludeIdxs[idx] || quotaCol[idx] <= 0 {
			continue
		}
		working[idx] = true
	}
	if len(working) == 0 {
		return 0, false
	}
	working = preferIfAvailable(working, options.PreferTagIdxs)
	return best(table, working, quotaCol, options.NowS)
}

func quotaSelectAny(table *AccountRuntimeTable, poolID int, options SelectOptions) (int, bool) {
	candidates := poolUnion(table, poolID)
	if len(candidates) == 0 {
		return 0, false
	}
	working := map[int]bool{}
	for idx := range candidates {
		if !options.ExcludeIdxs[idx] {
			working[idx] = true
		}
	}
	if len(working) == 0 {
		return 0, false
	}
	working = preferIfAvailable(working, options.PreferTagIdxs)
	return bestNoQuota(table, working, options.NowS)
}

func maybeResetWindows(
	table *AccountRuntimeTable,
	candidates map[int]bool,
	modeID int,
	resetCol []int,
	quotaCol []int,
	totalCol []int,
	windowCol []int,
	poolID int,
	nowS int,
) {
	if poolID != 0 {
		return
	}
	for idx := range candidates {
		reset := resetCol[idx]
		if reset == 0 || nowS < reset {
			continue
		}
		if table.PoolByIdx[idx] != poolID {
			continue
		}
		newTotal := totalCol[idx]
		windowS := windowCol[idx]
		if newTotal <= 0 || windowS <= 0 {
			continue
		}
		quotaCol[idx] = newTotal
		resetCol[idx] = nowS + windowS
	}
	_ = modeID
}

func best(table *AccountRuntimeTable, working map[int]bool, quotaCol []int, nowS int) (int, bool) {
	bestIdx := -1
	bestScore := -1e18
	for idx := range working {
		quota := quotaCol[idx]
		if quota <= 0 {
			continue
		}
		score := scoreCandidate(
			table.HealthByIdx[idx],
			quota,
			table.InflightByIdx[idx],
			table.FailCountByIdx[idx],
			table.LastUseAtByIdx[idx],
			nowS,
			true,
		)
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}
	if bestIdx < 0 {
		return 0, false
	}
	return bestIdx, true
}

func bestNoQuota(table *AccountRuntimeTable, working map[int]bool, nowS int) (int, bool) {
	bestIdx := -1
	bestScore := -1e18
	for idx := range working {
		score := scoreCandidate(
			table.HealthByIdx[idx],
			0,
			table.InflightByIdx[idx],
			table.FailCountByIdx[idx],
			table.LastUseAtByIdx[idx],
			nowS,
			false,
		)
		if score > bestScore {
			bestScore = score
			bestIdx = idx
		}
	}
	if bestIdx < 0 {
		return 0, false
	}
	return bestIdx, true
}

func scoreCandidate(health float64, quota int, inflight int, failCount int, lastUse int, nowS int, includeQuota bool) float64 {
	fails := failCount
	if fails > 10 {
		fails = 10
	}
	score := health*weightHealth - float64(inflight)*weightInflight - float64(fails)*weightFail
	if includeQuota {
		score += float64(quota) * weightQuota
	}
	if lastUse > 0 {
		ageS := nowS - lastUse
		if ageS < recentWindowS {
			score -= (1.0 - float64(ageS)/float64(recentWindowS)) * weightRecent
		}
	}
	return score
}

func randomSelect(table *AccountRuntimeTable, poolID int, options SelectOptions) (int, bool) {
	candidates := poolUnion(table, poolID)
	if len(candidates) == 0 {
		return 0, false
	}
	maxInflight := options.MaxInflight
	if maxInflight <= 0 {
		maxInflight = defaultMaxInflight
	}
	working := map[int]bool{}
	for idx := range candidates {
		if options.ExcludeIdxs[idx] {
			continue
		}
		if table.CoolingUntilSByIdx[idx] > options.NowS {
			continue
		}
		if table.InflightByIdx[idx] >= maxInflight {
			continue
		}
		working[idx] = true
	}
	if len(working) == 0 {
		return 0, false
	}
	working = preferIfAvailable(working, options.PreferTagIdxs)
	choices := make([]int, 0, len(working))
	for idx := range working {
		choices = append(choices, idx)
	}
	return choices[rand.Intn(len(choices))], true
}

func poolUnion(table *AccountRuntimeTable, poolID int) map[int]bool {
	out := map[int]bool{}
	for key, accounts := range table.ModeAvailable {
		if key.PoolID != poolID {
			continue
		}
		for idx := range accounts {
			out[idx] = true
		}
	}
	return out
}

func preferIfAvailable(working map[int]bool, prefer map[int]bool) map[int]bool {
	if len(prefer) == 0 {
		return working
	}
	preferred := map[int]bool{}
	for idx := range working {
		if prefer[idx] {
			preferred[idx] = true
		}
	}
	if len(preferred) == 0 {
		return working
	}
	return preferred
}
