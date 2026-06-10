package account

import "fmt"

type AccountUsageStats struct {
	UseCount  int `json:"use_count"`
	FailCount int `json:"fail_count"`
	SyncCount int `json:"sync_count"`
}

func (s AccountUsageStats) ToDict() map[string]int {
	return map[string]int{"use_count": s.UseCount, "fail_count": s.FailCount, "sync_count": s.SyncCount}
}

func AccountUsageStatsFromDict(data map[string]any) (AccountUsageStats, error) {
	useCount, err := intFromAny(data["use_count"], 0)
	if err != nil {
		return AccountUsageStats{}, fmt.Errorf("use_count: %w", err)
	}
	failCount, err := intFromAny(data["fail_count"], 0)
	if err != nil {
		return AccountUsageStats{}, fmt.Errorf("fail_count: %w", err)
	}
	syncCount, err := intFromAny(data["sync_count"], 0)
	if err != nil {
		return AccountUsageStats{}, fmt.Errorf("sync_count: %w", err)
	}
	return AccountUsageStats{UseCount: useCount, FailCount: failCount, SyncCount: syncCount}, nil
}

type AccountMutationResult struct {
	Upserted int `json:"upserted"`
	Patched  int `json:"patched"`
	Deleted  int `json:"deleted"`
	Revision int `json:"revision"`
}

type AccountPage struct {
	Items      []AccountRecord `json:"items"`
	Total      int             `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalPages int             `json:"total_pages"`
	Revision   int             `json:"revision"`
}

func NewAccountPage() AccountPage {
	return AccountPage{
		Items:      []AccountRecord{},
		Page:       1,
		PageSize:   50,
		TotalPages: 1,
	}
}

type AccountChangeSet struct {
	Revision      int             `json:"revision"`
	Items         []AccountRecord `json:"items"`
	DeletedTokens []string        `json:"deleted_tokens"`
	HasMore       bool            `json:"has_more"`
}

func NewAccountChangeSet() AccountChangeSet {
	return AccountChangeSet{
		Items:         []AccountRecord{},
		DeletedTokens: []string{},
	}
}

type RuntimeSnapshot struct {
	Revision int             `json:"revision"`
	Items    []AccountRecord `json:"items"`
}

func NewRuntimeSnapshot() RuntimeSnapshot {
	return RuntimeSnapshot{Items: []AccountRecord{}}
}
