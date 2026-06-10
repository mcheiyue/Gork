package storage

import (
	"context"
	"errors"
	"testing"
)

type fakeLockHandle struct {
	entered bool
	exited  bool
}

func (h *fakeLockHandle) Enter(_ context.Context) (LockHandle, error) {
	h.entered = true
	return h, nil
}

func (h *fakeLockHandle) Exit(_ context.Context) error {
	h.exited = true
	return nil
}

func TestLockHandleContract(t *testing.T) {
	var handle LockHandle = &fakeLockHandle{}
	entered, err := handle.Enter(context.Background())
	if err != nil {
		t.Fatalf("Enter returned error: %v", err)
	}
	if entered != handle {
		t.Fatalf("Enter returned %#v, want original handle", entered)
	}
	if err := handle.Exit(context.Background()); err != nil {
		t.Fatalf("Exit returned error: %v", err)
	}

	fake := handle.(*fakeLockHandle)
	if !fake.entered || !fake.exited {
		t.Fatalf("lock lifecycle not recorded: entered=%v exited=%v", fake.entered, fake.exited)
	}
}

func TestStorageErrorsSupportErrorsAs(t *testing.T) {
	err := NewLockAcquisitionError("lock timed out", nil)
	var storageErr *StorageError
	if !errors.As(err, &storageErr) {
		t.Fatalf("LockAcquisitionError should match StorageError via errors.As")
	}

	var lockErr *LockAcquisitionError
	if !errors.As(err, &lockErr) {
		t.Fatalf("expected LockAcquisitionError via errors.As")
	}
	if got := err.Error(); got != "lock timed out" {
		t.Fatalf("Error() = %q, want %q", got, "lock timed out")
	}
}
