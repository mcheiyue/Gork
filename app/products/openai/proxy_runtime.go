package openai

import (
	"context"

	controlproxy "github.com/dslzl/gork/app/control/proxy"
	proxydataplane "github.com/dslzl/gork/app/dataplane/proxy"
)

func defaultProxyTransportRuntime(ctx context.Context) (*controlproxy.ProxyDirectory, error) {
	return proxydataplane.GetTransportRuntime(ctx)
}

// assetProxyDirectoryAdapter bridges ProxyDirectory to the older AssetProxyRuntime
// shape used by local asset_upload (Acquire() (*ProxyLease, error) without options).
type assetProxyDirectoryAdapter struct {
	directory *controlproxy.ProxyDirectory
}

func (a assetProxyDirectoryAdapter) Acquire(ctx context.Context) (*controlproxy.ProxyLease, error) {
	if a.directory == nil {
		return nil, nil
	}
	lease, err := a.directory.Acquire(ctx, controlproxy.AcquireOptions{
		Scope:    controlproxy.ProxyScopeAsset,
		Kind:     controlproxy.RequestKindHTTP,
		Resource: false,
	})
	if err != nil {
		return nil, err
	}
	return &lease, nil
}

func (a assetProxyDirectoryAdapter) Feedback(ctx context.Context, lease controlproxy.ProxyLease, feedback controlproxy.ProxyFeedback) error {
	if a.directory == nil {
		return nil
	}
	return a.directory.Feedback(ctx, lease, feedback)
}
