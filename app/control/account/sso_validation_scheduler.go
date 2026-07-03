package account

import (
	"context"
	"sync"
	"time"

	platformconfig "github.com/dslzl/gork/app/platform/config"
)

var (
	ssoValidationEnabled = func() bool {
		return platformconfig.GlobalConfig.GetBool("account.sso_validation.enabled", false)
	}
	ssoValidationInterval = func() time.Duration {
		seconds := platformconfig.GlobalConfig.GetInt("account.sso_validation.interval_sec", 300)
		if seconds <= 0 {
			seconds = 300
		}
		return time.Duration(seconds) * time.Second
	}
)

type ssoValidationRunner interface {
	ValidateSSOBatch(context.Context, int, int) (SSOValidationResult, error)
}

type SSOValidationSchedulerOptions struct {
	Interval  time.Duration
	BatchSize int
}

type SSOValidationScheduler struct {
	runner    ssoValidationRunner
	interval  time.Duration
	batchSize int
	nextPage  int

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	wg      sync.WaitGroup
}

var ssoValidationSchedulerSingleton *SSOValidationScheduler

func NewSSOValidationScheduler(runner ssoValidationRunner, options SSOValidationSchedulerOptions) *SSOValidationScheduler {
	nextPage := 1
	return &SSOValidationScheduler{
		runner:    runner,
		interval:  options.Interval,
		batchSize: options.BatchSize,
		nextPage:  nextPage,
	}
}

func (s *SSOValidationScheduler) BindRunner(runner ssoValidationRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runner = runner
}

func (s *SSOValidationScheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *SSOValidationScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()
}

func (s *SSOValidationScheduler) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.running = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *SSOValidationScheduler) loop(ctx context.Context) {
	for {
		timer := time.NewTimer(s.intervalDuration())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			_, _ = s.runOnce(ctx)
		}
	}
}

func (s *SSOValidationScheduler) runOnce(ctx context.Context) (SSOValidationResult, error) {
	s.mu.Lock()
	runner := s.runner
	page := s.nextPage
	batchSize := s.effectiveBatchSizeLocked()
	s.mu.Unlock()
	if runner == nil {
		return SSOValidationResult{}, nil
	}
	if page < 1 {
		page = 1
	}
	result, err := runner.ValidateSSOBatch(ctx, page, batchSize)
	if err != nil {
		return SSOValidationResult{}, err
	}
	nextPage := result.NextPage
	if nextPage < 1 {
		nextPage = page + 1
	}
	s.mu.Lock()
	s.nextPage = nextPage
	s.mu.Unlock()
	return result, nil
}

func (s *SSOValidationScheduler) intervalDuration() time.Duration {
	s.mu.Lock()
	interval := s.interval
	s.mu.Unlock()
	if interval > 0 {
		return interval
	}
	return ssoValidationInterval()
}

func (s *SSOValidationScheduler) effectiveBatchSizeLocked() int {
	if s.batchSize > 0 {
		return s.batchSize
	}
	size := ssoValidationBatchSize()
	if size <= 0 {
		return 100
	}
	return size
}

func GetSSOValidationScheduler(runner ssoValidationRunner) *SSOValidationScheduler {
	if ssoValidationSchedulerSingleton == nil {
		ssoValidationSchedulerSingleton = NewSSOValidationScheduler(runner, SSOValidationSchedulerOptions{})
	} else {
		ssoValidationSchedulerSingleton.BindRunner(runner)
	}
	return ssoValidationSchedulerSingleton
}
