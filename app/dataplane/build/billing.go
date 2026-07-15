package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Billing 是 Build 上游 GET /billing 的宽松规范化结果。
// 数字字段不得 omitempty：free 号合法全 0，省略后 Admin 误判「无配额数据」。
type Billing struct {
	PlanCode             string    `json:"plan_code,omitempty"`
	PlanName             string    `json:"plan_name,omitempty"`
	MonthlyLimit         float64   `json:"monthly_limit"`
	Used                 float64   `json:"used"`
	OnDemandCap          float64   `json:"on_demand_cap"`
	OnDemandUsed         float64   `json:"on_demand_used"`
	PrepaidBalance       float64   `json:"prepaid_balance"`
	CreditUsagePercent   float64   `json:"credit_usage_percent"`
	IsUnifiedBillingUser bool      `json:"is_unified_billing_user"`
	TopUpMethod          string    `json:"top_up_method,omitempty"`
	BillingPeriodStart   string    `json:"billing_period_start,omitempty"`
	BillingPeriodEnd     string    `json:"billing_period_end,omitempty"`
	UsagePeriodType      string    `json:"usage_period_type,omitempty"`
	UsagePeriodStart     string    `json:"usage_period_start,omitempty"`
	UsagePeriodEnd       string    `json:"usage_period_end,omitempty"`
	SyncedAt             time.Time `json:"synced_at,omitempty"`
}

// GetBilling 拉取 /billing，并尝试合并 ?format=credits；credits 失败不阻断。
func (c *APIClient) GetBilling(ctx context.Context, accessToken string) (Billing, error) {
	monthly, err := c.getBillingOnce(ctx, accessToken, "")
	if err != nil {
		return Billing{}, err
	}
	if credits, cerr := c.getBillingOnce(ctx, accessToken, "format=credits"); cerr == nil {
		monthly = MergeBillingSnapshots(monthly, credits)
	}
	monthly.SyncedAt = time.Now().UTC()
	return monthly, nil
}

func (c *APIClient) getBillingOnce(ctx context.Context, accessToken, query string) (Billing, error) {
	path := "/billing"
	if query != "" {
		path += "?" + query
	}
	resp, err := c.do(ctx, http.MethodGet, path, nil, RequestMeta{AccessToken: accessToken}, false)
	if err != nil {
		return Billing{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Billing{}, fmt.Errorf("read build billing body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Billing{}, &UpstreamError{Status: resp.StatusCode, Body: truncateBody(body), Op: "get_billing"}
	}
	return ParseBilling(body)
}

// ParseBilling 宽松解析 Billing JSON（含 config 嵌套与多别名）。
func ParseBilling(data []byte) (Billing, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return Billing{}, fmt.Errorf("解析 Billing: %w", err)
	}
	original := root
	if nested, ok := root["config"].(map[string]any); ok {
		root = nested
	}
	planCode, planName := planValues(root)
	if planCode == "" || planName == "" {
		outerCode, outerName := planValues(original)
		if planCode == "" {
			planCode = outerCode
		}
		if planName == "" {
			planName = outerName
		}
	}
	result := Billing{
		PlanCode:           planCode,
		PlanName:           planName,
		MonthlyLimit:       numberValue(firstValue(root, "monthlyLimit", "monthly_limit")),
		Used:               numberValue(firstValue(root, "used", "totalUsed", "includedUsed")),
		OnDemandCap:        numberValue(firstValue(root, "onDemandCap", "on_demand_cap", "maxAmountPerMonth")),
		OnDemandUsed:       numberValue(firstValue(root, "onDemandUsed", "on_demand_used")),
		PrepaidBalance:     numberValue(firstValue(root, "prepaidBalance", "prepaid_balance")),
		CreditUsagePercent: numberValue(firstValue(root, "creditUsagePercent", "credit_usage_percent")),
		// 布尔/字符串可能在 config 内或与 config 同级（credits 响应）
		IsUnifiedBillingUser: boolValue(firstValue(root, "isUnifiedBillingUser", "is_unified_billing_user")),
		TopUpMethod:          stringValue(firstValue(root, "topUpMethod", "top_up_method")),
		BillingPeriodStart:   stringValue(firstValue(root, "billingPeriodStart", "billing_period_start")),
		BillingPeriodEnd:     stringValue(firstValue(root, "billingPeriodEnd", "billing_period_end")),
	}
	if !result.IsUnifiedBillingUser {
		result.IsUnifiedBillingUser = boolValue(firstValue(original, "isUnifiedBillingUser", "is_unified_billing_user"))
	}
	if result.TopUpMethod == "" {
		result.TopUpMethod = stringValue(firstValue(original, "topUpMethod", "top_up_method"))
	}
	if result.BillingPeriodStart == "" {
		result.BillingPeriodStart = stringValue(firstValue(original, "billingPeriodStart", "billing_period_start"))
	}
	if result.BillingPeriodEnd == "" {
		result.BillingPeriodEnd = stringValue(firstValue(original, "billingPeriodEnd", "billing_period_end"))
	}
	if currentPeriod, ok := root["currentPeriod"].(map[string]any); ok {
		result.UsagePeriodType = stringValue(currentPeriod["type"])
		result.UsagePeriodStart = stringValue(currentPeriod["start"])
		result.UsagePeriodEnd = stringValue(currentPeriod["end"])
	}
	if result.UsagePeriodType == "" {
		if currentPeriod, ok := original["currentPeriod"].(map[string]any); ok {
			result.UsagePeriodType = stringValue(currentPeriod["type"])
			result.UsagePeriodStart = stringValue(currentPeriod["start"])
			result.UsagePeriodEnd = stringValue(currentPeriod["end"])
		}
	}
	if result.CreditUsagePercent == 0 {
		switch {
		case result.OnDemandCap > 0:
			result.CreditUsagePercent = result.OnDemandUsed / result.OnDemandCap * 100
		case result.MonthlyLimit > 0:
			result.CreditUsagePercent = result.Used / result.MonthlyLimit * 100
		}
	}
	return result, nil
}

// MergeBillingSnapshots 用 credits 快照覆盖按需/周期字段；套餐名缺失时回填。
func MergeBillingSnapshots(monthly, credits Billing) Billing {
	if monthly.PlanCode == "" {
		monthly.PlanCode = credits.PlanCode
	}
	if monthly.PlanName == "" {
		monthly.PlanName = credits.PlanName
	}
	monthly.OnDemandCap = credits.OnDemandCap
	monthly.OnDemandUsed = credits.OnDemandUsed
	monthly.PrepaidBalance = credits.PrepaidBalance
	monthly.CreditUsagePercent = credits.CreditUsagePercent
	monthly.IsUnifiedBillingUser = credits.IsUnifiedBillingUser
	monthly.TopUpMethod = credits.TopUpMethod
	monthly.UsagePeriodType = credits.UsagePeriodType
	monthly.UsagePeriodStart = credits.UsagePeriodStart
	monthly.UsagePeriodEnd = credits.UsagePeriodEnd
	return monthly
}

func planValues(values map[string]any) (string, string) {
	code := stringValue(firstValue(values, "planCode", "plan_code", "subscriptionTier", "subscription_tier", "tier"))
	name := stringValue(firstValue(values, "planName", "plan_name", "subscriptionName", "subscription_name"))
	for _, key := range []string{"plan", "subscription", "membership"} {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if name == "" {
				name = typed
			}
		case map[string]any:
			if code == "" {
				code = stringValue(firstValue(typed, "code", "id", "tier", "slug"))
			}
			if name == "" {
				name = stringValue(firstValue(typed, "name", "displayName", "display_name", "label"))
			}
		}
	}
	return code, name
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		v, _ := typed.Float64()
		return v
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	case map[string]any:
		return numberValue(typed["val"])
	default:
		return 0
	}
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}
