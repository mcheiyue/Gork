//go:build windows

package app

import (
	"context"
	"os"
	"path/filepath"

	platform "github.com/jiujiu532/grok2api/app/platform"
)

func acquireAppMainSchedulerFileLock(context.Context) (Hook, error) {
	path := platform.DataPath(".scheduler.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return func(context.Context) error { return nil }, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return func(context.Context) error { return nil }, nil
	}
	return func(context.Context) error {
		return file.Close()
	}, nil
}
