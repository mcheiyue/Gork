package account

import (
	"sync"

	"github.com/jiujiu532/grok2api/app/platform/config"
)

var refreshRuntimeState = struct {
	sync.Mutex
	service         accountScheduledRefresher
	scheduler       *AccountRefreshScheduler
	schedulerLeader bool
	strategy        string
}{
	strategy: "random",
}

var runtimeRefreshEnabled = func() bool {
	return config.GlobalConfig.GetBool("account.refresh.enabled", false)
}

func SetRefreshService(service accountScheduledRefresher) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.service = service
}

func GetRefreshService() accountScheduledRefresher {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.service
}

func SetRefreshScheduler(scheduler *AccountRefreshScheduler) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.scheduler = scheduler
}

func GetRefreshScheduler() *AccountRefreshScheduler {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.scheduler
}

func SetRefreshSchedulerLeader(isLeader bool) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.schedulerLeader = isLeader
}

func IsRefreshSchedulerLeader() bool {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.schedulerLeader
}

func SetAccountSelectionStrategy(strategy string) {
	if strategy != "quota" && strategy != "random" {
		return
	}
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	refreshRuntimeState.strategy = strategy
}

func CurrentAccountSelectionStrategy() string {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.strategy
}

func ReconcileRefreshRuntime(enabled ...bool) string {
	refreshEnabled := runtimeRefreshEnabled()
	if len(enabled) > 0 {
		refreshEnabled = enabled[0]
	}
	targetStrategy := "random"
	if refreshEnabled {
		targetStrategy = "quota"
	}
	scheduler, leader := runtimeSchedulerState()
	SetAccountSelectionStrategy(targetStrategy)
	if scheduler != nil && leader {
		if refreshEnabled && !scheduler.IsRunning() {
			scheduler.Start()
		}
		if !refreshEnabled && scheduler.IsRunning() {
			scheduler.Stop()
		}
	}
	return targetStrategy
}

func runtimeSchedulerState() (*AccountRefreshScheduler, bool) {
	refreshRuntimeState.Lock()
	defer refreshRuntimeState.Unlock()
	return refreshRuntimeState.scheduler, refreshRuntimeState.schedulerLeader
}
