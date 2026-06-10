package runtime

import "time"

// NowMS returns the current wall-clock Unix time in milliseconds.
func NowMS() int64 {
	return time.Now().UnixMilli()
}

// NowS returns the current wall-clock Unix time in whole seconds.
func NowS() int64 {
	return time.Now().Unix()
}

// MSToS converts a millisecond timestamp to a second timestamp.
func MSToS(ms int64) int64 {
	q := ms / 1000
	r := ms % 1000
	if r != 0 && ms < 0 {
		return q - 1
	}
	return q
}

// SToMS converts a second timestamp to a millisecond timestamp.
func SToMS(s int64) int64 {
	return s * 1000
}
