//go:build windows

package storage

import "os"

func lockMediaFile(*os.File) error {
	return nil
}

func unlockMediaFile(*os.File) error {
	return nil
}
