package account

import (
	"context"
	"testing"
	"time"
)

type fakeScheduledRefresher struct {
	calls chan string
}

func (f *fakeScheduledRefresher) RefreshScheduled(_ context.Context, pool *string) (RefreshResult, error) {
	if pool != nil {
		select {
		case f.calls <- *pool:
		default:
		}
	}
	return RefreshResult{Checked: 1}, nil
}

func TestAccountRefreshIntervalDefaults(t *testing.T) {
	if got := accountRefreshInterval("basic", nil); got != 24*time.Hour {
		t.Fatalf("basic interval = %s, want 24h", got)
	}
	if got := accountRefreshInterval("super", nil); got != 2*time.Hour {
		t.Fatalf("super interval = %s, want 2h", got)
	}
	if got := accountRefreshInterval("heavy", nil); got != 2*time.Hour {
		t.Fatalf("heavy interval = %s, want 2h", got)
	}
}

func TestAccountRefreshIntervalReadsConfigAndHonorsOverrides(t *testing.T) {
	if accountRefreshIntervalConfigKeys["basic"] != "account.refresh.basic_interval_sec" ||
		accountRefreshIntervalConfigKeys["super"] != "account.refresh.super_interval_sec" ||
		accountRefreshIntervalConfigKeys["heavy"] != "account.refresh.heavy_interval_sec" {
		t.Fatalf("interval config keys = %#v", accountRefreshIntervalConfigKeys)
	}
	oldReader := accountRefreshIntervalSeconds
	accountRefreshIntervalSeconds = func(pool string, defaultSeconds int) int {
		if pool != "super" {
			return defaultSeconds
		}
		return 5
	}
	t.Cleanup(func() { accountRefreshIntervalSeconds = oldReader })

	if got := accountRefreshInterval("super", nil); got != 5*time.Second {
		t.Fatalf("configured super interval = %s, want 5s", got)
	}
	overrides := map[string]time.Duration{"super": time.Millisecond}
	if got := accountRefreshInterval("super", overrides); got != time.Millisecond {
		t.Fatalf("override super interval = %s, want 1ms", got)
	}
}

func TestAccountRefreshSchedulerLifecycleAndSingleton(t *testing.T) {
	service1 := &fakeScheduledRefresher{calls: make(chan string, 10)}
	service2 := &fakeScheduledRefresher{calls: make(chan string, 10)}
	scheduler := NewAccountRefreshScheduler(service1)
	if scheduler.IsRunning() {
		t.Fatal("new scheduler should not be running")
	}
	scheduler.Start()
	if !scheduler.IsRunning() {
		t.Fatal("scheduler should be running after Start")
	}
	scheduler.BindService(service2)
	if scheduler.service != service2 {
		t.Fatal("BindService should update scheduler service")
	}
	scheduler.BindService(service1)
	scheduler.Start()
	if len(scheduler.cancelByPool) != 3 {
		t.Fatalf("duplicate Start changed loop count to %d, want 3", len(scheduler.cancelByPool))
	}
	scheduler.Stop()
	if scheduler.IsRunning() {
		t.Fatal("scheduler should stop after Stop")
	}

	accountRefreshSchedulerSingleton = nil
	first := GetAccountRefreshScheduler(service1)
	second := GetAccountRefreshScheduler(service2)
	if first != second || second.service != service2 {
		t.Fatal("singleton should be reused and rebound to latest service")
	}
	second.Stop()
}

func TestAccountRefreshSchedulerRunsPoolLoop(t *testing.T) {
	service := &fakeScheduledRefresher{calls: make(chan string, 10)}
	scheduler := NewAccountRefreshScheduler(service)
	scheduler.refreshPool(context.Background(), "heavy")
	if pool := <-service.calls; pool != "heavy" {
		t.Fatalf("direct refreshPool = %q, want heavy", pool)
	}
	scheduler.intervals = map[string]time.Duration{
		"basic": time.Millisecond,
		"super": time.Hour,
		"heavy": time.Hour,
	}
	scheduler.Start()
	defer scheduler.Stop()

	select {
	case pool := <-service.calls:
		if pool != "basic" {
			t.Fatalf("scheduled pool = %q, want basic", pool)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("scheduler did not run basic pool loop")
	}
}
