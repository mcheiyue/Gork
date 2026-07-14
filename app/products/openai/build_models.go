package openai

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dslzl/gork/app/control/model"
)

// buildModelsCache 缓存上游 ListModels，避免 /v1/models 每次打上游。
type buildModelsCache struct {
	mu        sync.Mutex
	ids       []string
	expiresAt time.Time
	ttl       time.Duration
}

var defaultBuildModelsCache = &buildModelsCache{ttl: 10 * time.Minute}

// listBuildModelSpecs 在开关开启且有 active 账号时返回 build/<id> 规格。
// 上游失败时返回空切片（不污染静态列表）；单模型 Resolve 仍走 BuildSpecFromName。
func listBuildModelSpecs(ctx context.Context) []model.ModelSpec {
	if !buildFeatureEnabled() {
		return nil
	}
	dir := buildAccountDir()
	if dir == nil {
		return nil
	}
	accounts, err := dir.ListActive(ctx, time.Now().UTC())
	if err != nil || len(accounts) == 0 {
		return nil
	}
	ids := defaultBuildModelsCache.loadOrFetch(ctx, accounts[0].AccessToken)
	if len(ids) == 0 {
		return nil
	}
	specs := make([]model.ModelSpec, 0, len(ids))
	for _, id := range ids {
		name := model.BuildModelPrefix + id
		if spec, ok := model.BuildSpecFromName(name); ok {
			specs = append(specs, spec)
		}
	}
	return specs
}

func (c *buildModelsCache) loadOrFetch(ctx context.Context, accessToken string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if now.Before(c.expiresAt) && len(c.ids) > 0 {
		return append([]string{}, c.ids...)
	}
	if strings.TrimSpace(accessToken) == "" {
		return append([]string{}, c.ids...)
	}
	client := buildAPIClient()
	lister, ok := client.(buildModelLister)
	if !ok {
		// CreateResponse-only fake in tests：保持旧缓存
		return append([]string{}, c.ids...)
	}
	ids, err := lister.ListModels(ctx, accessToken)
	if err != nil || len(ids) == 0 {
		c.expiresAt = now.Add(30 * time.Second)
		return append([]string{}, c.ids...)
	}
	sort.Strings(ids)
	c.ids = ids
	ttl := c.ttl
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	c.expiresAt = now.Add(ttl)
	return append([]string{}, c.ids...)
}

type buildModelLister interface {
	ListModels(ctx context.Context, accessToken string) ([]string, error)
}

// buildAPIClient 默认实现已支持 ListModels；此处适配 defaultBuildAPIClient 返回值。
// 测试可注入仅实现 CreateResponse 的 fake。

// resetBuildModelsCacheForTest 仅测试用。
func resetBuildModelsCacheForTest() {
	defaultBuildModelsCache.mu.Lock()
	defer defaultBuildModelsCache.mu.Unlock()
	defaultBuildModelsCache.ids = nil
	defaultBuildModelsCache.expiresAt = time.Time{}
}
