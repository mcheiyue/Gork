package build

// HasQuotaSnapshot 表示是否已有可用的配额快照（未同步则不门禁）。
func (b Billing) HasQuotaSnapshot() bool {
	if !b.SyncedAt.IsZero() {
		return true
	}
	return b.MonthlyLimit > 0 ||
		b.OnDemandCap > 0 ||
		b.CreditUsagePercent > 0 ||
		b.PrepaidBalance > 0 ||
		b.Used > 0 ||
		b.OnDemandUsed > 0
}

// QuotaExhausted 在已有快照时判断额度是否耗尽。
// 无快照 → false（不拦截选号）；有 prepaid / 未满月额度 / 未满 on-demand 任一可用 → false。
func (b Billing) QuotaExhausted() bool {
	if !b.HasQuotaSnapshot() {
		return false
	}
	if b.CreditUsagePercent >= 100 {
		return true
	}
	if b.PrepaidBalance > 0 {
		return false
	}
	if b.OnDemandCap > 0 && b.OnDemandUsed < b.OnDemandCap {
		return false
	}
	if b.MonthlyLimit > 0 && b.Used < b.MonthlyLimit {
		return false
	}
	// 仅当存在明确上限且全部打满时才视为耗尽
	if b.MonthlyLimit > 0 || b.OnDemandCap > 0 || b.CreditUsagePercent > 0 {
		return true
	}
	return false
}
