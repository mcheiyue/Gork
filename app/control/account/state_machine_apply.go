package account

import "strconv"

type feedbackState struct {
	base           AccountRecord
	quotaSet       AccountQuotaSet
	status         AccountStatus
	ext            map[string]any
	lastFailAt     *int64
	lastFailReason *string
	lastUseAt      *int64
	lastSyncAt     *int64
	useCount       int
	failCount      int
	syncCount      int
	stateReason    *string
}

func newFeedbackState(record AccountRecord, feedback AccountFeedback) (*feedbackState, error) {
	quotaSet, err := record.QuotaSet()
	if err != nil {
		return nil, err
	}
	return &feedbackState{
		base:           cloneAccountRecord(record),
		quotaSet:       quotaSet,
		status:         record.Status,
		ext:            cloneAnyMap(record.Ext),
		lastFailAt:     cloneInt64Ptr(record.LastFailAt),
		lastFailReason: cloneStringPtr(record.LastFailReason),
		lastUseAt:      cloneInt64Ptr(record.LastUseAt),
		lastSyncAt:     cloneInt64Ptr(record.LastSyncAt),
		useCount:       record.UsageUseCount,
		failCount:      record.UsageFailCount,
		syncCount:      record.UsageSyncCount,
		stateReason:    cloneStringPtr(record.StateReason),
	}, nil
}

func (s *feedbackState) applyQuota(feedback AccountFeedback) {
	if feedback.QuotaWindow != nil {
		s.quotaSet.Set(feedback.ModeID, cloneQuotaWindow(*feedback.QuotaWindow))
		s.lastSyncAt = int64ValuePtr(feedback.At)
		s.syncCount++
		return
	}
	if feedback.Kind == FeedbackKindSuccess {
		s.decrementQuota(feedback.ModeID)
		return
	}
	if feedback.Kind == FeedbackKindRateLimited {
		s.rateLimitQuota(feedback)
	}
}

func (s *feedbackState) applyCounters(feedback AccountFeedback) {
	if feedback.Kind == FeedbackKindSuccess && feedback.ApplyUsage {
		s.useCount++
		s.lastUseAt = int64ValuePtr(feedback.At)
		return
	}
	if countsAsFailure(feedback.Kind) {
		s.failCount++
		s.lastFailAt = int64ValuePtr(feedback.At)
		s.lastFailReason = stringValuePtr(feedbackFailureReason(feedback))
	}
}

func (s *feedbackState) applyStatus(feedback AccountFeedback, policy StatePolicy) {
	switch feedback.Kind {
	case FeedbackKindUnauthorized:
		s.applyUnauthorized(feedback)
	case FeedbackKindForbidden:
		s.applyForbidden(feedback, policy)
	case FeedbackKindRateLimited:
		s.applyRateLimited(feedback, policy)
	case FeedbackKindSuccess:
		s.applySuccess(feedback)
	case FeedbackKindDisable:
		s.disable(feedback.At, defaultString(feedback.Reason, "operator_disabled"))
	case FeedbackKindRestore:
		s.restore()
	}
}

func (s *feedbackState) record(updatedAt int64) AccountRecord {
	updated := s.base
	updated.Status = s.status
	updated.Quota = s.quotaSet.ToDict()
	updated.UsageUseCount = s.useCount
	updated.UsageFailCount = s.failCount
	updated.UsageSyncCount = s.syncCount
	updated.LastUseAt = cloneInt64Ptr(s.lastUseAt)
	updated.LastFailAt = cloneInt64Ptr(s.lastFailAt)
	updated.LastFailReason = cloneStringPtr(s.lastFailReason)
	updated.LastSyncAt = cloneInt64Ptr(s.lastSyncAt)
	updated.StateReason = cloneStringPtr(s.stateReason)
	updated.Ext = cloneAnyMap(s.ext)
	updated.UpdatedAt = updatedAt
	return updated
}

func (s *feedbackState) decrementQuota(modeID int) {
	if window := s.quotaSet.Get(modeID); window != nil {
		updated := cloneQuotaWindow(*window)
		updated.Remaining = clampInt(updated.Remaining-1, 0, updated.Total)
		s.quotaSet.Set(modeID, updated)
	}
}

func (s *feedbackState) rateLimitQuota(feedback AccountFeedback) {
	window := s.quotaSet.Get(feedback.ModeID)
	if window == nil {
		return
	}
	resetAt := feedback.At + int64(window.WindowSeconds)*1000
	if feedback.RetryAfterMS != nil && *feedback.RetryAfterMS != 0 {
		resetAt = feedback.At + *feedback.RetryAfterMS
	}
	updated := cloneQuotaWindow(*window)
	updated.Remaining = 0
	updated.ResetAt = &resetAt
	s.quotaSet.Set(feedback.ModeID, updated)
}

func countsAsFailure(kind FeedbackKind) bool {
	return kind != FeedbackKindSuccess && kind != FeedbackKindRestore &&
		kind != FeedbackKindDisable && kind != FeedbackKindDelete
}

func feedbackFailureReason(feedback AccountFeedback) string {
	if feedback.Reason != "" {
		return feedback.Reason
	}
	if feedback.StatusCode == nil || *feedback.StatusCode == 0 {
		return ""
	}
	return strconv.Itoa(*feedback.StatusCode)
}
