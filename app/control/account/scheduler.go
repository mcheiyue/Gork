package account

import (
	"context"
	"sync"
	"time"

	"github.com/jiujiu532/grok2api/app/platform/config"
)

var accountRefreshPoolOrder = []string{"basic", "super", "heavy"}

var accountRefreshDefaultIntervals = map[string]time.Duration{
	"basic": 24 * time.Hour,
	"super": 2 * time.Hour,
	"heavy": 2 * time.Hour,
}

var accountRefreshIntervalConfigKeys = map[string]string{
	"basic": "account.refresh.basic_interval_sec",
	"super": "account.refresh.super_interval_sec",
	"heavy": "account.refresh.heavy_interval_sec",
}

var accountRefreshIntervalSeconds = func(pool string, defaultSeconds int) int {
	if key, ok := accountRefreshIntervalConfigKeys[pool]; ok {
		return config.GlobalConfig.GetInt(key, defaultSeconds)
	}
	return defaultSeconds
}

type accountScheduledRefresher interface {
	RefreshScheduled(context.Context, *string) (RefreshResult, error)
}

type AccountRefreshScheduler struct {
	service      accountScheduledRefresher
	intervals    map[string]time.Duration
	cancelByPool map[string]context.CancelFunc
	mu           sync.Mutex
}

var accountRefreshSchedulerSingleton *AccountRefreshScheduler

func NewAccountRefreshScheduler(refreshService accountScheduledRefresher) *AccountRefreshScheduler {
	return &AccountRefreshScheduler{
		service:      refreshService,
		intervals:    map[string]time.Duration{},
		cancelByPool: map[string]context.CancelFunc{},
	}
}

func (s *AccountRefreshScheduler) BindService(refreshService accountScheduledRefresher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.service = refreshService
}

func (s *AccountRefreshScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cancelByPool) > 0
}

func (s *AccountRefreshScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cancelByPool) > 0 {
		return
	}
	for _, pool := range accountRefreshPoolOrder {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelByPool[pool] = cancel
		go s.loop(ctx, pool)
	}
}

func (s *AccountRefreshScheduler) Stop() {
	s.mu.Lock()
	cancels := s.cancelByPool
	s.cancelByPool = map[string]context.CancelFunc{}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (s *AccountRefreshScheduler) loop(ctx context.Context, pool string) {
	for {
		timer := time.NewTimer(accountRefreshInterval(pool, s.intervals))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.refreshPool(ctx, pool)
		}
	}
}

func (s *AccountRefreshScheduler) refreshPool(ctx context.Context, pool string) {
	s.mu.Lock()
	service := s.service
	s.mu.Unlock()
	if service == nil {
		return
	}
	_, _ = service.RefreshScheduled(ctx, &pool)
}

func GetAccountRefreshScheduler(refreshService accountScheduledRefresher) *AccountRefreshScheduler {
	if accountRefreshSchedulerSingleton == nil {
		accountRefreshSchedulerSingleton = NewAccountRefreshScheduler(refreshService)
	} else {
		accountRefreshSchedulerSingleton.BindService(refreshService)
	}
	return accountRefreshSchedulerSingleton
}

func accountRefreshInterval(pool string, overrides map[string]time.Duration) time.Duration {
	if overrides != nil {
		if interval, ok := overrides[pool]; ok {
			return interval
		}
	}
	if interval, ok := accountRefreshDefaultIntervals[pool]; ok {
		return time.Duration(accountRefreshIntervalSeconds(pool, int(interval/time.Second))) * time.Second
	}
	fallback := accountRefreshDefaultIntervals["basic"]
	return time.Duration(accountRefreshIntervalSeconds("basic", int(fallback/time.Second))) * time.Second
}
