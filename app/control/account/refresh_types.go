package account

import (
	"context"
	"sync"
	"time"

	"github.com/jiujiu532/grok2api/app/dataplane/reverse/protocol"
	appruntime "github.com/jiujiu532/grok2api/app/platform/runtime"
)

var refreshNowMS = appruntime.NowMS

var allRefreshModeIDs = []int{0, 1, 2, 3, 4, 5}

type RefreshResult struct {
	Checked     int
	Refreshed   int
	Recovered   int
	Expired     int
	Disabled    int
	RateLimited int
	Failed      int
}

func (r *RefreshResult) Merge(other RefreshResult) {
	r.Checked += other.Checked
	r.Refreshed += other.Refreshed
	r.Recovered += other.Recovered
	r.Expired += other.Expired
	r.Disabled += other.Disabled
	r.RateLimited += other.RateLimited
	r.Failed += other.Failed
}

type AccountRefreshRepository interface {
	GetAccounts(context.Context, []string) ([]AccountRecord, error)
	PatchAccounts(context.Context, []AccountPatch) (AccountMutationResult, error)
	RuntimeSnapshot(context.Context) (RuntimeSnapshot, error)
}

type AccountRefreshOptions struct {
	Fetcher             protocol.UsageFetcher
	UsageConcurrency    int
	OnDemandMinInterval time.Duration
}

type AccountRefreshService struct {
	repo                AccountRefreshRepository
	fetcher             protocol.UsageFetcher
	usageConcurrency    int
	onDemandMinInterval time.Duration
	mu                  sync.Mutex
	onDemandRunning     bool
	onDemandLast        time.Time
}

func NewAccountRefreshService(repo AccountRefreshRepository, options AccountRefreshOptions) *AccountRefreshService {
	concurrency := options.UsageConcurrency
	if concurrency <= 0 {
		concurrency = 50
	}
	minInterval := options.OnDemandMinInterval
	if minInterval <= 0 {
		minInterval = 300 * time.Second
	}
	return &AccountRefreshService{
		repo:                repo,
		fetcher:             options.Fetcher,
		usageConcurrency:    concurrency,
		onDemandMinInterval: minInterval,
	}
}

func isRefreshManageable(record AccountRecord, now int64) bool {
	if record.IsDeleted() {
		return false
	}
	status := deriveRefreshStatus(record, now)
	return status == AccountStatusActive || status == AccountStatusCooling
}

func deriveRefreshStatus(record AccountRecord, now int64) AccountStatus {
	if record.Status != AccountStatusCooling {
		return record.Status
	}
	value, ok := record.Ext["cooldown_until"]
	if !ok || value == nil {
		return AccountStatusCooling
	}
	cooldownUntil, err := int64FromAny(value)
	if err != nil || now < cooldownUntil {
		return AccountStatusCooling
	}
	return AccountStatusActive
}
