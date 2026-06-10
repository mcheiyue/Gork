package storage

import "context"

// LockHandle represents a lock lifecycle for a storage backend.
type LockHandle interface {
	Enter(context.Context) (LockHandle, error)
	Exit(context.Context) error
}

// StorageError is the base error type for storage-layer failures.
type StorageError struct {
	Message string
	Cause   error
}

func (e *StorageError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "storage error"
}

func (e *StorageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// LockAcquisitionError is returned when a lock cannot be acquired in time.
type LockAcquisitionError struct {
	*StorageError
}

// NewLockAcquisitionError creates a lock acquisition error.
func NewLockAcquisitionError(message string, cause error) *LockAcquisitionError {
	return &LockAcquisitionError{
		StorageError: &StorageError{
			Message: message,
			Cause:   cause,
		},
	}
}

func (e *LockAcquisitionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.StorageError
}
