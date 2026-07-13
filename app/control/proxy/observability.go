package proxy

import (
	"cmp"
	"slices"
)

func (d *ProxyDirectory) ObservabilityStatus() map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.clock()
	bundles := make([]map[string]any, 0, len(d.bundles))
	stateCounts := map[string]int{}
	for key, bundle := range d.bundles {
		state := clearanceBundleStateName(bundle.State)
		stateCounts[state]++
		item := map[string]any{
			"affinity":           key.Affinity,
			"clearance_host":     key.ClearanceHost,
			"bundle_id":          bundle.BundleID,
			"state":              state,
			"refresh_count":      bundle.RefreshCount,
			"last_refresh_error": bundle.LastRefreshError,
		}
		if bundle.LastRefreshAt != nil {
			item["last_refresh_at"] = *bundle.LastRefreshAt
			item["age_ms"] = now - *bundle.LastRefreshAt
		}
		if bundle.ExpiresAt != nil {
			item["expires_at"] = *bundle.ExpiresAt
			item["expires_in_ms"] = *bundle.ExpiresAt - now
		}
		bundles = append(bundles, item)
	}
	slices.SortFunc(bundles, func(leftBundle, rightBundle map[string]any) int {
		left := leftBundle["affinity"].(string) + "@" + leftBundle["clearance_host"].(string)
		right := rightBundle["affinity"].(string) + "@" + rightBundle["clearance_host"].(string)
		return cmp.Compare(left, right)
	})
	return map[string]any{
		"clearance_mode":     string(d.clearanceMode),
		"bundle_count":       len(d.bundles),
		"state_counts":       stateCounts,
		"inflight_refreshes": len(d.refreshEvents),
		"bundles":            bundles,
	}
}

func clearanceBundleStateName(state ClearanceBundleState) string {
	switch state {
	case ClearanceBundleValid:
		return "valid"
	case ClearanceBundleStale:
		return "stale"
	case ClearanceBundleInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}
