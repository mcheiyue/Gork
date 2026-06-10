//go:build !windows

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

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
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, nil
		}
		return func(context.Context) error { return nil }, nil
	}
	return func(context.Context) error {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return err
	}, nil
}
