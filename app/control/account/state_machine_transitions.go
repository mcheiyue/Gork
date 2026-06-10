package account

func (s *feedbackState) applyUnauthorized(feedback AccountFeedback) {
	if !feedback.ConfirmExpired {
		return
	}
	reason := defaultString(feedback.Reason, "token_expired")
	s.status = AccountStatusExpired
	s.stateReason = stringValuePtr(reason)
	s.ext[expiredAtKey] = feedback.At
	s.ext[expiredReasonKey] = reason
}

func (s *feedbackState) applyForbidden(feedback AccountFeedback, policy StatePolicy) {
	strikes := intFromExt(s.ext[forbiddenStrikeKey]) + 1
	s.ext[forbiddenStrikeKey] = strikes
	if strikes < policy.ForbiddenStrikes {
		return
	}
	s.disable(feedback.At, defaultString(feedback.Reason, "forbidden"))
}

func (s *feedbackState) applyRateLimited(feedback AccountFeedback, policy StatePolicy) {
	cooldownMS := policy.DefaultCoolingMS
	if feedback.RetryAfterMS != nil && *feedback.RetryAfterMS != 0 {
		cooldownMS = *feedback.RetryAfterMS
	}
	reason := defaultString(feedback.Reason, "rate_limited")
	s.status = AccountStatusCooling
	s.stateReason = stringValuePtr(reason)
	s.ext[cooldownUntilKey] = feedback.At + cooldownMS
	s.ext[cooldownReasonKey] = reason
}

func (s *feedbackState) applySuccess(feedback AccountFeedback) {
	if s.status != AccountStatusCooling {
		return
	}
	cooldownUntil, ok := s.ext[cooldownUntilKey]
	if !ok || cooldownUntil == nil {
		return
	}
	until, err := int64FromAny(cooldownUntil)
	if err != nil || feedback.At < until {
		return
	}
	s.status = AccountStatusActive
	s.stateReason = nil
	delete(s.ext, cooldownUntilKey)
	delete(s.ext, cooldownReasonKey)
	delete(s.ext, forbiddenStrikeKey)
}

func (s *feedbackState) disable(at int64, reason string) {
	s.status = AccountStatusDisabled
	s.stateReason = stringValuePtr(reason)
	s.ext[disabledAtKey] = at
	s.ext[disabledReasonKey] = reason
}

func (s *feedbackState) restore() {
	s.status = AccountStatusActive
	s.stateReason = nil
	clearFailureExt(s.ext)
	s.quotaSet = DefaultQuotaSet(s.base.Pool)
}

func cloneAccountRecord(record AccountRecord) AccountRecord {
	record.Tags = append([]string{}, record.Tags...)
	record.Quota = cloneAnyMap(record.Quota)
	record.Ext = cloneAnyMap(record.Ext)
	record.LastUseAt = cloneInt64Ptr(record.LastUseAt)
	record.LastFailAt = cloneInt64Ptr(record.LastFailAt)
	record.LastFailReason = cloneStringPtr(record.LastFailReason)
	record.LastSyncAt = cloneInt64Ptr(record.LastSyncAt)
	record.LastClearAt = cloneInt64Ptr(record.LastClearAt)
	record.StateReason = cloneStringPtr(record.StateReason)
	record.DeletedAt = cloneInt64Ptr(record.DeletedAt)
	return record
}

func intFromExt(value any) int {
	parsed, err := intFromAny(value, 0)
	if err != nil {
		return 0
	}
	return parsed
}

func int64ValuePtr(value int64) *int64 {
	return &value
}

func stringValuePtr(value string) *string {
	return &value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
