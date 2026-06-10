package proxy

import (
	"context"
	"testing"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

type fakeRuntimeDirectory struct {
	egressMode       controlproxy.EgressMode
	acquiredOptions  controlproxy.AcquireOptions
	feedbackLease    controlproxy.ProxyLease
	feedbackFeedback controlproxy.ProxyFeedback
}

func (f *fakeRuntimeDirectory) Acquire(_ context.Context, options controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	f.acquiredOptions = options
	return controlproxy.NewProxyLease("lease-1"), nil
}

func (f *fakeRuntimeDirectory) Feedback(_ context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	f.feedbackLease = lease
	f.feedbackFeedback = feedback
	return nil
}

func (f *fakeRuntimeDirectory) EgressMode() controlproxy.EgressMode {
	return f.egressMode
}

func TestProxyRuntimeAcquireDelegatesToDirectory(t *testing.T) {
	directory := &fakeRuntimeDirectory{egressMode: controlproxy.EgressModeProxyPool}
	runtime := NewProxyRuntime(directory)
	origin := "https://grok.com"

	lease, err := runtime.Acquire(context.Background(), AcquireOptions{
		Scope:           controlproxy.ProxyScopeAsset,
		Kind:            controlproxy.RequestKindWebSocket,
		Resource:        true,
		ClearanceOrigin: &origin,
	})
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if lease.LeaseID != "lease-1" {
		t.Fatalf("lease = %#v", lease)
	}
	if directory.acquiredOptions.Scope != controlproxy.ProxyScopeAsset ||
		directory.acquiredOptions.Kind != controlproxy.RequestKindWebSocket ||
		!directory.acquiredOptions.Resource ||
		directory.acquiredOptions.ClearanceOrigin != origin {
		t.Fatalf("acquire options = %#v", directory.acquiredOptions)
	}
}

func TestProxyRuntimeAcquireUsesPythonDefaults(t *testing.T) {
	directory := &fakeRuntimeDirectory{}
	runtime := NewProxyRuntime(directory)

	if _, err := runtime.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if directory.acquiredOptions.Scope != controlproxy.ProxyScopeApp ||
		directory.acquiredOptions.Kind != controlproxy.RequestKindHTTP ||
		directory.acquiredOptions.Resource ||
		directory.acquiredOptions.ClearanceOrigin != "" {
		t.Fatalf("default acquire options = %#v", directory.acquiredOptions)
	}
}

func TestGetProxyRuntimeUsesControlProxyDirectorySingleton(t *testing.T) {
	defaultRuntimeMu.Lock()
	defaultRuntime = nil
	defaultRuntimeMu.Unlock()

	first, err := GetProxyRuntime(context.Background())
	if err != nil {
		t.Fatalf("GetProxyRuntime returned error: %v", err)
	}
	second, err := GetProxyRuntime(context.Background())
	if err != nil {
		t.Fatalf("GetProxyRuntime returned error on second call: %v", err)
	}
	if first == nil || first.directory == nil {
		t.Fatalf("first runtime = %#v", first)
	}
	if first != second {
		t.Fatalf("GetProxyRuntime should return cached runtime")
	}
	if first.HasProxy() {
		t.Fatalf("default proxy runtime should use direct control directory")
	}
}

func TestProxyRuntimeFeedbackDelegatesToDirectory(t *testing.T) {
	directory := &fakeRuntimeDirectory{}
	runtime := NewProxyRuntime(directory)
	lease := controlproxy.NewProxyLease("lease-1")
	feedback := controlproxy.NewProxyFeedback(controlproxy.ProxyFeedbackSuccess)

	if err := runtime.Feedback(context.Background(), lease, feedback); err != nil {
		t.Fatalf("Feedback returned error: %v", err)
	}
	if directory.feedbackLease.LeaseID != "lease-1" || directory.feedbackFeedback.Kind != controlproxy.ProxyFeedbackSuccess {
		t.Fatalf("feedback delegation lease=%#v feedback=%#v", directory.feedbackLease, directory.feedbackFeedback)
	}
}

func TestProxyRuntimeHasProxyMatchesPython(t *testing.T) {
	direct := NewProxyRuntime(&fakeRuntimeDirectory{egressMode: controlproxy.EgressModeDirect})
	if direct.HasProxy() {
		t.Fatalf("HasProxy should be false in direct mode")
	}
	pooled := NewProxyRuntime(&fakeRuntimeDirectory{egressMode: controlproxy.EgressModeProxyPool})
	if !pooled.HasProxy() {
		t.Fatalf("HasProxy should be true outside direct mode")
	}
}
