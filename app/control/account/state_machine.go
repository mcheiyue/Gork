package account

const (
	cooldownUntilKey   = "cooldown_until"
	cooldownReasonKey  = "cooldown_reason"
	disabledAtKey      = "disabled_at"
	disabledReasonKey  = "disabled_reason"
	expiredAtKey       = "expired_at"
	expiredReasonKey   = "expired_reason"
	forbiddenStrikeKey = "forbidden_strikes"
)

func DeriveStatus(record AccountRecord, now ...int64) AccountStatus {
	if record.Status != AccountStatusCooling {
		return record.Status
	}
	cooldownUntil, ok := record.Ext[cooldownUntilKey]
	if !ok || cooldownUntil == nil {
		return AccountStatusCooling
	}
	ts := stateMachineNowMS()
	if len(now) > 0 {
		ts = now[0]
	}
	until, err := int64FromAny(cooldownUntil)
	if err != nil || ts < until {
		return AccountStatusCooling
	}
	return AccountStatusActive
}

func IsSelectable(record AccountRecord, modeID int, now ...int64) bool {
	if record.IsDeleted() || DeriveStatus(record, now...) != AccountStatusActive {
		return false
	}
	quotaSet, err := record.QuotaSet()
	if err != nil {
		return false
	}
	window := quotaSet.Get(modeID)
	return window != nil && !window.IsExhausted()
}

func IsManageable(record AccountRecord, now ...int64) bool {
	if record.IsDeleted() {
		return false
	}
	status := DeriveStatus(record, now...)
	return status == AccountStatusActive || status == AccountStatusCooling
}

func ApplyFeedback(record AccountRecord, feedback AccountFeedback, policy StatePolicy) (AccountRecord, error) {
	policy = normalizeStatePolicy(policy)
	state, err := newFeedbackState(record, feedback)
	if err != nil {
		return AccountRecord{}, err
	}
	state.applyQuota(feedback)
	state.applyCounters(feedback)
	state.applyStatus(feedback, policy)
	return state.record(feedback.At), nil
}

func ClearFailures(record AccountRecord) AccountRecord {
	updated := cloneAccountRecord(record)
	updated.Status = AccountStatusActive
	updated.UsageFailCount = 0
	updated.LastFailAt = nil
	updated.LastFailReason = nil
	updated.StateReason = nil
	updated.UpdatedAt = stateMachineNowMS()
	clearFailureExt(updated.Ext)
	return updated
}

func clearFailureExt(ext map[string]any) {
	delete(ext, cooldownUntilKey)
	delete(ext, cooldownReasonKey)
	delete(ext, disabledAtKey)
	delete(ext, disabledReasonKey)
	delete(ext, expiredAtKey)
	delete(ext, expiredReasonKey)
	delete(ext, forbiddenStrikeKey)
}
