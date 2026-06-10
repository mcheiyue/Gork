package account

func setQuotaPatch(patch *AccountPatch, modeID int, value map[string]any) {
	switch modeID {
	case 0:
		patch.QuotaAuto = value
	case 1:
		patch.QuotaFast = value
	case 2:
		patch.QuotaExpert = value
	case 3:
		patch.QuotaHeavy = value
	case 4:
		patch.QuotaGrok43 = value
	case 5:
		patch.QuotaConsole = value
	}
}

func hasQuotaPatch(patch AccountPatch) bool {
	return patch.QuotaAuto != nil || patch.QuotaFast != nil || patch.QuotaExpert != nil ||
		patch.QuotaHeavy != nil || patch.QuotaGrok43 != nil || patch.QuotaConsole != nil
}

func applyFallbackWindowPatch(patch *AccountPatch, quotaSet AccountQuotaSet, pool string, modeID int, now int64) {
	existing := quotaSet.Get(modeID)
	if existing == nil {
		return
	}
	if existing.Source == QuotaSourceReal {
		setQuotaPatch(patch, modeID, estimatedDecrementWindow(*existing).ToDict())
		return
	}
	if existing.IsWindowExpired(now) {
		if fallback := DefaultQuotaWindow(pool, modeID); fallback != nil {
			setQuotaPatch(patch, modeID, defaultResetWindow(*fallback, now).ToDict())
		}
	}
}

func estimatedDecrementWindow(window QuotaWindow) QuotaWindow {
	window.Remaining = clampInt(window.Remaining-1, 0, window.Total)
	window.Source = QuotaSourceEstimated
	return window
}

func defaultResetWindow(window QuotaWindow, now int64) QuotaWindow {
	resetAt := now + int64(window.WindowSeconds)*1000
	return QuotaWindow{
		Remaining:     window.Total,
		Total:         window.Total,
		WindowSeconds: window.WindowSeconds,
		ResetAt:       &resetAt,
		SyncedAt:      &now,
		Source:        QuotaSourceDefault,
	}
}

func rateLimitedWindowPatch(window QuotaWindow, now int64) QuotaWindow {
	resetAt := now + int64(maxInt(window.WindowSeconds, 1))*1000
	if window.ResetAt != nil && *window.ResetAt > now {
		resetAt = *window.ResetAt
	}
	return QuotaWindow{
		Remaining:     0,
		Total:         window.Total,
		WindowSeconds: window.WindowSeconds,
		ResetAt:       &resetAt,
		SyncedAt:      cloneInt64Ptr(window.SyncedAt),
		Source:        QuotaSourceEstimated,
	}
}

func localUseWindowPatch(pool string, modeID int, window QuotaWindow, now int64) QuotaWindow {
	if window.IsWindowExpired(now) {
		if fallback := DefaultQuotaWindow(pool, modeID); fallback != nil {
			resetAt := now + int64(fallback.WindowSeconds)*1000
			return QuotaWindow{
				Remaining:     clampInt(fallback.Total-1, 0, fallback.Total),
				Total:         fallback.Total,
				WindowSeconds: fallback.WindowSeconds,
				ResetAt:       &resetAt,
				SyncedAt:      &now,
				Source:        QuotaSourceDefault,
			}
		}
	}
	window.Remaining = clampInt(window.Remaining-1, 0, window.Total)
	if modeID == 5 {
		if window.ResetAt == nil && window.Remaining <= 15 && window.WindowSeconds > 0 {
			resetAt := now + int64(window.WindowSeconds)*1000
			window.ResetAt = &resetAt
		}
	} else if window.ResetAt == nil && window.WindowSeconds > 0 {
		resetAt := now + int64(window.WindowSeconds)*1000
		window.ResetAt = &resetAt
	}
	window.Source = QuotaSourceEstimated
	return window
}

func isKnownRefreshMode(modeID int) bool {
	return modeID >= 0 && modeID <= 5
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func swallowRefreshError(err error) error {
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
