package runtime

import (
	"fmt"
	"sync/atomic"
)

var idCounter atomic.Int64

// NextID returns a process-local monotonically increasing integer id.
func NextID() int64 {
	return idCounter.Add(1)
}

// NextHex returns a zero-padded hex string derived from the monotonic counter.
func NextHex(length ...int) string {
	width := 12
	if len(length) > 0 {
		width = length[0]
	}
	if width < 0 {
		panic("negative hex width")
	}
	return fmt.Sprintf("%0*x", width, NextID())
}
