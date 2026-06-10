package admin

import (
	"github.com/jiujiu532/grok2api/app/platform/config"
	"github.com/jiujiu532/grok2api/app/platform/storage"
)

type adminCacheStore interface {
	Clear(storage.MediaType) (int, error)
	Delete(storage.MediaType, string) (bool, error)
}

type adminCacheConfig struct{}

var (
	adminCacheImageDir  = storage.ImageFilesDir
	adminCacheVideoDir  = storage.VideoFilesDir
	adminCacheConfigInt = func(key string, fallback int) int {
		if adminRouterConfig != nil {
			return adminRouterConfig.GetInt(key, fallback)
		}
		return config.GlobalConfig.GetInt(key, fallback)
	}
	adminCacheStoreProvider = func() adminCacheStore {
		return storage.NewLocalMediaCacheStore(storage.LocalMediaCacheOptions{Config: adminCacheConfig{}})
	}
)

func (adminCacheConfig) GetInt(key string, fallback int) int {
	return adminCacheConfigInt(key, fallback)
}
