package transport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

func normalizeImagineRoundOptions(options imagineRoundOptions) imagineRoundOptions {
	if options.AspectRatio == "" {
		options.AspectRatio = "2:3"
	}
	if options.Needed <= 0 {
		options.Needed = 1
	}
	if options.StreamTimeout == 0 {
		options.StreamTimeout = defaultImagineStreamTimeout
	}
	if options.RoundTimeout == 0 {
		options.RoundTimeout = defaultImagineTimeout
	}
	if options.InterRoundWait == 0 {
		options.InterRoundWait = defaultImagineInterRoundWait
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return options
}

func probeImagineClosed(ctx context.Context, ws ImagineWebSocketConnection, wait time.Duration) (bool, error) {
	msg, err := ws.Receive(ctx, wait)
	if err != nil {
		if imagineIsReceiveTimeout(err) {
			return false, nil
		}
		return true, nil
	}
	return msg.Type == ImagineWebSocketClosedMessage || msg.Type == ImagineWebSocketErrorMessage, nil
}

func imagineAllSlotsDone(slots map[string]*imagineSlot) bool {
	for _, slot := range slots {
		if !slot.Done {
			return false
		}
	}
	return len(slots) > 0
}

func imagineIsReceiveTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func imagineString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func imagineInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func imagineBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true"
	default:
		return false
	}
}

func imagineProgress(value any) int {
	progress := 50
	switch typed := value.(type) {
	case float64:
		progress = int(typed)
	case int:
		progress = typed
	case string:
		if parsed, err := strconv.ParseFloat(typed, 64); err == nil {
			progress = int(parsed)
		}
	}
	if progress < 10 {
		return 10
	}
	if progress > 99 {
		return 99
	}
	return progress
}

func minImagineDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func imaginePrefix(value string, length int) string {
	if len(value) < length {
		return value
	}
	return value[:length]
}

func newImagineRequestID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(data[:])
}
