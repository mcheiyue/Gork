package account

import (
	"testing"
	"time"
)

func TestRefreshRuntimeAccessors(t *testing.T) {
	service := &fakeScheduledRefresher{calls: make(chan string, 1)}
	scheduler := NewAccountRefreshScheduler(service)
	SetRefreshService(nil)
	SetRefreshScheduler(nil)
	SetRefreshSchedulerLeader(false)

	SetRefreshService(service)
	SetRefreshScheduler(scheduler)
	SetRefreshSchedulerLeader(true)

	if GetRefreshService() != service {
		t.Fatal("refresh service accessor did not return registered service")
	}
	if GetRefreshScheduler() != scheduler {
		t.Fatal("refresh scheduler accessor did not return registered scheduler")
	}
	if !IsRefreshSchedulerLeader() {
		t.Fatal("leader accessor should return true")
	}
}

func TestReconcileRefreshRuntimeStartsAndStopsLeaderScheduler(t *testing.T) {
	service := &fakeScheduledRefresher{calls: make(chan string, 1)}
	scheduler := NewAccountRefreshScheduler(service)
	scheduler.intervals = map[string]time.Duration{"basic": time.Hour, "super": time.Hour, "heavy": time.Hour}
	SetRefreshScheduler(scheduler)
	SetRefreshSchedulerLeader(true)
	SetAccountSelectionStrategy("random")
	t.Cleanup(func() {
		scheduler.Stop()
		SetRefreshScheduler(nil)
		SetRefreshSchedulerLeader(false)
		SetAccountSelectionStrategy("random")
	})

	if got := ReconcileRefreshRuntime(true); got != "quota" {
		t.Fatalf("enabled strategy = %q, want quota", got)
	}
	if CurrentAccountSelectionStrategy() != "quota" || !scheduler.IsRunning() {
		t.Fatalf("enabled reconcile did not set quota/running")
	}
	if got := ReconcileRefreshRuntime(false); got != "random" {
		t.Fatalf("disabled strategy = %q, want random", got)
	}
	if CurrentAccountSelectionStrategy() != "random" || scheduler.IsRunning() {
		t.Fatalf("disabled reconcile did not set random/stopped")
	}
}

func TestReconcileRefreshRuntimeDoesNotStartNonLeaderScheduler(t *testing.T) {
	service := &fakeScheduledRefresher{calls: make(chan string, 1)}
	scheduler := NewAccountRefreshScheduler(service)
	SetRefreshScheduler(scheduler)
	SetRefreshSchedulerLeader(false)
	SetAccountSelectionStrategy("random")
	t.Cleanup(func() {
		scheduler.Stop()
		SetRefreshScheduler(nil)
		SetRefreshSchedulerLeader(false)
		SetAccountSelectionStrategy("random")
	})

	if got := ReconcileRefreshRuntime(true); got != "quota" {
		t.Fatalf("enabled strategy = %q, want quota", got)
	}
	if scheduler.IsRunning() {
		t.Fatal("non-leader reconcile should not start scheduler")
	}
}

func TestReconcileRefreshRuntimeReadsConfigWhenEnabledOmitted(t *testing.T) {
	service := &fakeScheduledRefresher{calls: make(chan string, 1)}
	scheduler := NewAccountRefreshScheduler(service)
	scheduler.intervals = map[string]time.Duration{"basic": time.Hour, "super": time.Hour, "heavy": time.Hour}
	oldReader := runtimeRefreshEnabled
	runtimeRefreshEnabled = func() bool { return true }
	SetRefreshScheduler(scheduler)
	SetRefreshSchedulerLeader(true)
	SetAccountSelectionStrategy("random")
	t.Cleanup(func() {
		runtimeRefreshEnabled = oldReader
		scheduler.Stop()
		SetRefreshScheduler(nil)
		SetRefreshSchedulerLeader(false)
		SetAccountSelectionStrategy("random")
	})

	if got := ReconcileRefreshRuntime(); got != "quota" {
		t.Fatalf("config-enabled strategy = %q, want quota", got)
	}
	if CurrentAccountSelectionStrategy() != "quota" || !scheduler.IsRunning() {
		t.Fatalf("config-enabled reconcile did not set quota/running")
	}
	if got := ReconcileRefreshRuntime(false); got != "random" {
		t.Fatalf("explicit disabled strategy = %q, want random", got)
	}
	if CurrentAccountSelectionStrategy() != "random" || scheduler.IsRunning() {
		t.Fatalf("explicit disabled reconcile did not override config reader")
	}
}
