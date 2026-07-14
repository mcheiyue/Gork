package build

import (
	"testing"
	"time"
)

func TestQuotaExhaustedNoSnapshot(t *testing.T) {
	if (Billing{}).QuotaExhausted() {
		t.Fatal("empty billing must not gate")
	}
}

func TestQuotaExhaustedMonthly(t *testing.T) {
	b := Billing{SyncedAt: time.Now().UTC(), MonthlyLimit: 100, Used: 100}
	if !b.QuotaExhausted() {
		t.Fatal("monthly full should exhaust")
	}
	b.Used = 50
	if b.QuotaExhausted() {
		t.Fatal("monthly remaining should allow")
	}
}

func TestQuotaExhaustedPrepaidAndOnDemand(t *testing.T) {
	b := Billing{SyncedAt: time.Now().UTC(), MonthlyLimit: 100, Used: 100, PrepaidBalance: 5}
	if b.QuotaExhausted() {
		t.Fatal("prepaid should allow")
	}
	b.PrepaidBalance = 0
	b.OnDemandCap = 20
	b.OnDemandUsed = 5
	if b.QuotaExhausted() {
		t.Fatal("on-demand remaining should allow")
	}
	b.OnDemandUsed = 20
	if !b.QuotaExhausted() {
		t.Fatal("all caps full should exhaust")
	}
}

func TestQuotaExhaustedCreditPercent(t *testing.T) {
	b := Billing{SyncedAt: time.Now().UTC(), CreditUsagePercent: 100}
	if !b.QuotaExhausted() {
		t.Fatal("100% credit usage should exhaust")
	}
}
