package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dslzl/gork/app/control/buildaccount"
)

type buildAccountListFilter struct {
	Status     string
	Expired    *bool
	HasBilling *bool
	Query      string
}

func parseBuildAccountListFilter(r *http.Request) buildAccountListFilter {
	q := r.URL.Query()
	filter := buildAccountListFilter{
		Status: strings.ToLower(strings.TrimSpace(q.Get("status"))),
		Query:  strings.TrimSpace(q.Get("q")),
	}
	if raw := strings.TrimSpace(q.Get("expired")); raw != "" {
		v := raw == "1" || strings.EqualFold(raw, "true")
		filter.Expired = &v
	}
	if raw := strings.TrimSpace(q.Get("has_billing")); raw != "" {
		v := raw == "1" || strings.EqualFold(raw, "true")
		filter.HasBilling = &v
	}
	return filter
}

func matchBuildAccountFilter(acc buildaccount.Account, filter buildAccountListFilter, now time.Time) bool {
	if filter.Status != "" && !strings.EqualFold(acc.Status, filter.Status) {
		return false
	}
	if filter.Expired != nil && isBuildTokenExpired(acc, now) != *filter.Expired {
		return false
	}
	if filter.HasBilling != nil && hasBuildBilling(acc) != *filter.HasBilling {
		return false
	}
	if filter.Query == "" {
		return true
	}
	needle := strings.ToLower(filter.Query)
	hay := strings.ToLower(strings.Join([]string{acc.Name, acc.Email, acc.UserID, strconv.FormatInt(acc.ID, 10)}, " "))
	return strings.Contains(hay, needle)
}

func isBuildTokenExpired(acc buildaccount.Account, now time.Time) bool {
	if acc.ExpiresAt.IsZero() {
		return false
	}
	return !acc.ExpiresAt.After(now)
}

func hasBuildBilling(acc buildaccount.Account) bool {
	return !acc.BillingSynced.IsZero() || acc.Billing.PlanCode != "" || acc.Billing.MonthlyLimit > 0 || acc.Billing.Used > 0
}

func buildAccountFacets(accounts []buildaccount.Account, now time.Time) map[string]any {
	var active, disabled, cooling, expiredToken, withBilling int
	for _, acc := range accounts {
		switch acc.Status {
		case buildaccount.StatusActive:
			active++
		case buildaccount.StatusDisabled:
			disabled++
		case buildaccount.StatusCooling:
			cooling++
		}
		if isBuildTokenExpired(acc, now) {
			expiredToken++
		}
		if hasBuildBilling(acc) {
			withBilling++
		}
	}
	return map[string]any{
		"all":           len(accounts),
		"active":        active,
		"disabled":      disabled,
		"cooling":       cooling,
		"expired_token": expiredToken,
		"with_billing":  withBilling,
	}
}
