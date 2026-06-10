package shared

import platformruntime "github.com/jiujiu532/grok2api/app/platform/runtime"

// NowMS returns the current wall-clock Unix time in milliseconds.
func NowMS() int64 {
	return platformruntime.NowMS()
}

// NowS returns the current wall-clock Unix time in whole seconds.
func NowS() int64 {
	return platformruntime.NowS()
}

// MSToS converts a millisecond timestamp to a second timestamp.
func MSToS(ms int64) int64 {
	return platformruntime.MSToS(ms)
}

// SToMS converts a second timestamp to a millisecond timestamp.
func SToMS(s int64) int64 {
	return platformruntime.SToMS(s)
}
