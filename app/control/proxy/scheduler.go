package proxy

import (
	"context"
	"sync"
	"time"

	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type ProxyClearanceDirectory interface {
	Load(ctx context.Context) error
	WarmUp(ctx context.Context) error
	RefreshClearanceSafe(ctx context.Context) error
}

type SchedulerConfig interface {
	GetInt(key string, defaultValue int) int
}

type SchedulerOptions struct {
	Config SchedulerConfig
}

type globalSchedulerConfig struct{}

func (globalSchedulerConfig) GetInt(key string, defaultValue int) int {
	return platformconfig.GlobalConfig.GetInt(key, defaultValue)
}

type ProxyClearanceScheduler struct {
	directory ProxyClearanceDirectory
	config    SchedulerConfig

	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
}

func NewProxyClearanceScheduler(directory ProxyClearanceDirectory, options ...SchedulerOptions) *ProxyClearanceScheduler {
	scheduler := &ProxyClearanceScheduler{directory: directory}
	if len(options) > 0 {
		scheduler.config = options[0].Config
	}
	return scheduler
}

func (s *ProxyClearanceScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true
	go s.loop(loopCtx)
}

func (s *ProxyClearanceScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *ProxyClearanceScheduler) loop(ctx context.Context) {
	s.warmUp(ctx)
	for s.isRunning() {
		timer := time.NewTimer(time.Duration(s.interval()) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if !s.isRunning() {
			return
		}
		s.refresh(ctx)
	}
}

func (s *ProxyClearanceScheduler) warmUp(ctx context.Context) {
	if err := s.directory.Load(ctx); err != nil {
		return
	}
	_ = s.directory.WarmUp(ctx)
}

func (s *ProxyClearanceScheduler) refresh(ctx context.Context) {
	if err := s.directory.Load(ctx); err != nil {
		return
	}
	_ = s.directory.RefreshClearanceSafe(ctx)
}

func (s *ProxyClearanceScheduler) interval() int {
	cfg := s.config
	if cfg == nil {
		cfg = globalSchedulerConfig{}
	}
	return cfg.GetInt("proxy.clearance.refresh_interval", 600)
}

func (s *ProxyClearanceScheduler) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
