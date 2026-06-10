package proxy

import (
	"context"
	"sync"

	controlproxy "github.com/jiujiu532/grok2api/app/control/proxy"
)

type AcquireOptions struct {
	Scope           controlproxy.ProxyScope
	Kind            controlproxy.RequestKind
	Resource        bool
	ClearanceOrigin *string
}

type ProxyRuntimeDirectory interface {
	Acquire(ctx context.Context, options controlproxy.AcquireOptions) (controlproxy.ProxyLease, error)
	Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error
	EgressMode() controlproxy.EgressMode
}

type controlProxyDirectoryAdapter struct {
	directory *controlproxy.ProxyDirectory
}

func (a controlProxyDirectoryAdapter) Acquire(ctx context.Context, options controlproxy.AcquireOptions) (controlproxy.ProxyLease, error) {
	return a.directory.Acquire(ctx, options)
}

func (a controlProxyDirectoryAdapter) Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	return a.directory.Feedback(ctx, lease, feedback)
}

func (a controlProxyDirectoryAdapter) EgressMode() controlproxy.EgressMode {
	return a.directory.EgressMode()
}

type ProxyRuntime struct {
	directory ProxyRuntimeDirectory
}

var (
	defaultRuntimeMu sync.Mutex
	defaultRuntime   *ProxyRuntime
)

func NewProxyRuntime(directory ProxyRuntimeDirectory) *ProxyRuntime {
	return &ProxyRuntime{directory: directory}
}

func GetProxyRuntime(ctx context.Context, options ...controlproxy.DirectoryOptions) (*ProxyRuntime, error) {
	directory, err := controlproxy.GetProxyDirectory(ctx, options...)
	if err != nil {
		return nil, err
	}
	defaultRuntimeMu.Lock()
	defer defaultRuntimeMu.Unlock()
	if defaultRuntime == nil {
		defaultRuntime = NewProxyRuntime(controlProxyDirectoryAdapter{directory: directory})
	}
	return defaultRuntime, nil
}

func (r *ProxyRuntime) Acquire(ctx context.Context, options ...AcquireOptions) (controlproxy.ProxyLease, error) {
	opts := AcquireOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	opts = normalizeAcquireOptions(opts)
	controlOptions := controlproxy.AcquireOptions{
		Scope:    opts.Scope,
		Kind:     opts.Kind,
		Resource: opts.Resource,
	}
	if opts.ClearanceOrigin != nil {
		controlOptions.ClearanceOrigin = *opts.ClearanceOrigin
	}
	return r.directory.Acquire(ctx, controlOptions)
}

func normalizeAcquireOptions(options AcquireOptions) AcquireOptions {
	if options.Scope == "" {
		options.Scope = controlproxy.ProxyScopeApp
	}
	if options.Kind == "" {
		options.Kind = controlproxy.RequestKindHTTP
	}
	return options
}

func (r *ProxyRuntime) Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	return r.directory.Feedback(ctx, lease, feedback)
}

func (r *ProxyRuntime) HasProxy() bool {
	return r.directory.EgressMode() != controlproxy.EgressModeDirect
}
