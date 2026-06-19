package transport

import (
	"context"

	"github.com/dslzl/gork/app/dataplane/reverse/runtime"
)

type HTTPUsageFetcher struct{}

func (HTTPUsageFetcher) FetchUsage(ctx context.Context, token, modeName string) (map[string]any, error) {
	payload := []byte(`{"modelName":"` + modeName + `"}`)
	return PostJSON(ctx, runtime.RateLimits, token, payload)
}
