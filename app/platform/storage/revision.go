package storage

import "sync"

// RevisionTracker is a thread-safe monotonic revision counter.
type RevisionTracker struct {
	mu       sync.Mutex
	revision int64
}

// NewRevisionTracker creates a tracker with the provided initial revision.
func NewRevisionTracker(initial int64) *RevisionTracker {
	return &RevisionTracker{revision: initial}
}

// Current returns the current revision.
func (t *RevisionTracker) Current() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.revision
}

// Bump increments and returns the new revision.
func (t *RevisionTracker) Bump() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.revision++
	return t.revision
}

// Set force-sets the revision, for example after loading persisted state.
func (t *RevisionTracker) Set(value int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.revision = value
}
