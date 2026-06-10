package account

import appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"

type AccountLease struct {
	LeaseID    int64
	Idx        int
	Token      string
	PoolID     int
	ModeID     int
	SelectedAt int
}

func NewLease(idx int, token string, poolID int, modeID int, selectedAt int) AccountLease {
	return AccountLease{
		LeaseID:    appruntime.NextID(),
		Idx:        idx,
		Token:      token,
		PoolID:     poolID,
		ModeID:     modeID,
		SelectedAt: selectedAt,
	}
}
