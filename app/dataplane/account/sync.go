package account

import (
	"context"
	"fmt"

	controlaccount "github.com/jiujiu532/grok2api/app/control/account"
	"github.com/jiujiu532/grok2api/app/dataplane/shared"
	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

const defaultSyncBatchLimit = controlaccount.AccountScanChangesDefaultLimit

func Bootstrap(ctx context.Context, repository controlaccount.AccountRepository) (*AccountRuntimeTable, error) {
	snapshot, err := repository.RuntimeSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	table := MakeEmptyTable()
	for _, record := range snapshot.Items {
		if record.IsDeleted() {
			continue
		}
		slot, tags, err := recordToSlot(record)
		if err != nil {
			return nil, err
		}
		slot.Token = record.Token
		slot.Tags = tags
		table.AppendSlot(slot)
	}
	table.Revision = snapshot.Revision
	return table, nil
}

func ApplyChanges(ctx context.Context, table *AccountRuntimeTable, repository controlaccount.AccountRepository, batchLimit ...int) (bool, error) {
	limit := defaultSyncBatchLimit
	if len(batchLimit) > 0 {
		limit = batchLimit[0]
	}
	changed := false
	for {
		changeset, err := repository.ScanChanges(ctx, table.Revision, limit)
		if err != nil {
			return changed, err
		}
		for _, token := range changeset.DeletedTokens {
			if idx, ok := table.IdxByToken[token]; ok {
				markDeleted(table, idx)
				changed = true
			}
		}
		for _, record := range changeset.Items {
			if record.IsDeleted() {
				if idx, ok := table.IdxByToken[record.Token]; ok {
					markDeleted(table, idx)
					changed = true
				}
				continue
			}
			slot, tags, err := recordToSlot(record)
			if err != nil {
				return changed, err
			}
			slot.Token = record.Token
			slot.Tags = tags
			if existing, ok := table.IdxByToken[record.Token]; ok {
				table.UpdateSlot(existing, slot, oldTagsFor(table, existing))
			} else {
				table.AppendSlot(slot)
			}
			changed = true
		}
		if changeset.Revision > table.Revision {
			table.Revision = changeset.Revision
		}
		if !changeset.HasMore {
			break
		}
	}
	return changed, nil
}

func recordToSlot(record controlaccount.AccountRecord) (AccountSlot, []string, error) {
	quotaSet, err := record.QuotaSet()
	if err != nil {
		return AccountSlot{}, nil, fmt.Errorf("account quota set for %s: %w", record.Token, err)
	}
	normalized := controlaccount.NormalizeQuotaSet(record.Pool, quotaSet)
	statusID := StatusActive
	if mapped, ok := shared.StatusStringToID[controlaccount.DeriveStatus(record).String()]; ok {
		statusID = mapped
	}
	poolID := 0
	if mapped, ok := shared.PoolStringToID[record.Pool]; ok {
		poolID = mapped
	}
	heavyQuota, heavyTotal, heavyWindow, heavyReset := optionalWindowValues(normalized.Heavy)
	grokQuota, grokTotal, grokWindow, grokReset := optionalWindowValues(normalized.Grok43)
	consoleQuota, consoleTotal, consoleWindow, consoleReset := optionalWindowValues(normalized.Console)
	return AccountSlot{
		Token:         record.Token,
		PoolID:        poolID,
		StatusID:      statusID,
		QuotaAuto:     remaining(normalized.Auto),
		QuotaFast:     remaining(normalized.Fast),
		QuotaExpert:   remaining(normalized.Expert),
		QuotaHeavy:    heavyQuota,
		QuotaGrok43:   grokQuota,
		QuotaConsole:  consoleQuota,
		TotalAuto:     total(normalized.Auto),
		TotalFast:     total(normalized.Fast),
		TotalExpert:   total(normalized.Expert),
		TotalHeavy:    heavyTotal,
		TotalGrok43:   grokTotal,
		TotalConsole:  consoleTotal,
		WindowAuto:    windowSeconds(normalized.Auto),
		WindowFast:    windowSeconds(normalized.Fast),
		WindowExpert:  windowSeconds(normalized.Expert),
		WindowHeavy:   heavyWindow,
		WindowGrok43:  grokWindow,
		WindowConsole: consoleWindow,
		ResetAuto:     resetSeconds(normalized.Auto),
		ResetFast:     resetSeconds(normalized.Fast),
		ResetExpert:   resetSeconds(normalized.Expert),
		ResetHeavy:    heavyReset,
		ResetGrok43:   grokReset,
		ResetConsole:  consoleReset,
		Health:        1.0,
		LastUseS:      optionalMSToS(record.LastUseAt),
		LastFailS:     optionalMSToS(record.LastFailAt),
		FailCount:     record.UsageFailCount,
		Tags:          append([]string{}, record.Tags...),
	}, append([]string{}, record.Tags...), nil
}

func markDeleted(table *AccountRuntimeTable, idx int) {
	table.removeFromIndexes(idx)
	table.StatusByIdx[idx] = StatusDeleted
	if table.Size > 0 {
		table.Size--
	}
}

func oldTagsFor(table *AccountRuntimeTable, idx int) []string {
	tags := []string{}
	for tag, bucket := range table.TagIdx {
		if bucket[idx] {
			tags = append(tags, tag)
		}
	}
	return tags
}

func optionalWindowValues(window *controlaccount.QuotaWindow) (int, int, int, int) {
	if window == nil {
		return -1, 0, 0, 0
	}
	return remaining(*window), total(*window), windowSeconds(*window), resetSeconds(*window)
}

func remaining(window controlaccount.QuotaWindow) int {
	return maxInt(0, window.Remaining)
}

func total(window controlaccount.QuotaWindow) int {
	return maxInt(0, window.Total)
}

func windowSeconds(window controlaccount.QuotaWindow) int {
	return maxInt(0, window.WindowSeconds)
}

func resetSeconds(window controlaccount.QuotaWindow) int {
	if window.ResetAt == nil {
		return 0
	}
	return int(appruntime.MSToS(*window.ResetAt))
}

func optionalMSToS(value *int64) int {
	if value == nil {
		return 0
	}
	return int(appruntime.MSToS(*value))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
