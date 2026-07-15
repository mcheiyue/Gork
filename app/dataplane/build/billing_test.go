package build

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseBillingNestedConfigAndAliases(t *testing.T) {
	raw := []byte(`{
		"config": {
			"plan": {"code":"pro","name":"Pro"},
			"monthlyLimit": 100,
			"used": 25,
			"onDemandCap": 50,
			"onDemandUsed": 10,
			"currentPeriod": {"type":"week","start":"s","end":"e"}
		}
	}`)
	got, err := ParseBilling(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanCode != "pro" || got.PlanName != "Pro" {
		t.Fatalf("plan=%#v", got)
	}
	if got.MonthlyLimit != 100 || got.Used != 25 {
		t.Fatalf("quota=%#v", got)
	}
	if got.CreditUsagePercent != 20 { // 10/50*100
		t.Fatalf("percent=%v", got.CreditUsagePercent)
	}
	if got.UsagePeriodType != "week" {
		t.Fatalf("period=%#v", got)
	}
}

func TestMergeBillingSnapshotsPrefersCreditsPeriod(t *testing.T) {
	monthly := Billing{PlanCode: "m", MonthlyLimit: 100, Used: 10}
	credits := Billing{
		PlanName: "Credits", OnDemandCap: 40, OnDemandUsed: 8,
		CreditUsagePercent: 20, UsagePeriodType: "week",
		UsagePeriodStart: "a", UsagePeriodEnd: "b",
	}
	got := MergeBillingSnapshots(monthly, credits)
	if got.PlanCode != "m" || got.PlanName != "Credits" {
		t.Fatalf("plan=%#v", got)
	}
	if got.OnDemandCap != 40 || got.UsagePeriodType != "week" {
		t.Fatalf("merged=%#v", got)
	}
	if got.MonthlyLimit != 100 {
		t.Fatalf("monthly should keep limit=%v", got.MonthlyLimit)
	}
}

func TestParseBillingValZeroShapeKeepsZerosInJSON(t *testing.T) {
	// 生产 free 号真实形态：config.*.val=0 + credits 周周期
	raw := []byte(`{
		"config": {
			"monthlyLimit": {"val": 0},
			"used": {"val": 0},
			"onDemandCap": {"val": 0},
			"billingPeriodStart": "2026-07-01T00:00:00Z",
			"billingPeriodEnd": "2026-08-01T00:00:00Z"
		},
		"isUnifiedBillingUser": true,
		"topUpMethod": "TOP_UP_METHOD_SAVED_PAYMENT_METHOD"
	}`)
	got, err := ParseBilling(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.MonthlyLimit != 0 || got.Used != 0 || got.OnDemandCap != 0 {
		t.Fatalf("zeros=%#v", got)
	}
	if !got.IsUnifiedBillingUser || got.TopUpMethod == "" {
		t.Fatalf("flags=%#v", got)
	}
	if got.BillingPeriodStart == "" {
		t.Fatalf("period missing: %#v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(encoded, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"monthly_limit", "used", "on_demand_cap", "on_demand_used", "prepaid_balance", "credit_usage_percent"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("json omitted zero field %s: %s", key, encoded)
		}
	}
}

func TestAPIClientGetBillingMergesCredits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.RawQuery == "format=credits" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"onDemandCap": 30, "onDemandUsed": 6, "creditUsagePercent": 20,
				"currentPeriod": map[string]any{"type": "week", "start": "s", "end": "e"},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"planCode": "basic", "planName": "Basic", "monthlyLimit": 100, "used": 5,
		})
	}))
	defer srv.Close()

	client := NewAPIClient(srv.Client(), ClientConfig{BaseURL: srv.URL})
	got, err := client.GetBilling(t.Context(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if got.PlanCode != "basic" || got.OnDemandCap != 30 || got.UsagePeriodType != "week" {
		t.Fatalf("%#v", got)
	}
	if got.SyncedAt.IsZero() {
		t.Fatal("expected SyncedAt")
	}
}
