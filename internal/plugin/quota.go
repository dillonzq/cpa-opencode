package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/dillonzq/cpa-opencode/internal/catalog"
	"github.com/dillonzq/cpa-opencode/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const quotaResponseLimit = 2 << 20

type quotaBudget struct {
	ID       string   `json:"ruleID"`
	Limit    *float64 `json:"limitDollars"`
	Spent    *float64 `json:"spentDollars"`
	Scope    string   `json:"scope"`
	Per      string   `json:"per"`
	Coverage string   `json:"coverage"`
	Shared   bool     `json:"shared"`
	Picked   bool     `json:"picked"`
	ResetsAt string   `json:"resetsAt"`
	Models   []struct {
		ProviderID string   `json:"providerID"`
		ModelID    string   `json:"modelID"`
		Name       string   `json:"name"`
		Spent      *float64 `json:"spentDollars"`
	} `json:"models"`
	Vendors []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"vendors"`
	InputPriceAtLeast *float64 `json:"inputPriceAtLeast"`
}

// FetchQuota reads fresh data for exactly the credential selected by CPA.
// It has no scheduler state and never clears an execution cooldown.
func (m *Manager) FetchQuota(parent context.Context, req pluginapi.QuotaFetchRequest) (pluginapi.QuotaFetchResponse, error) {
	empty := pluginapi.QuotaFetchResponse{}
	if req.Provider != ProviderID {
		return empty, fmt.Errorf("unsupported quota provider")
	}
	key := strings.TrimSpace(req.Attributes["api_key"])
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()
	if key == "" || !cfg.HasCredential(key) {
		return empty, fmt.Errorf("selected quota credential is unavailable")
	}
	if m.bridge == nil {
		return empty, fmt.Errorf("quota bridge unavailable")
	}
	timeout := cfg.QuotaTimeout
	if timeout <= 0 {
		timeout = config.DefaultQuotaTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	headers := http.Header{"Authorization": {"Bearer " + key}, "Accept": {"application/json"}}
	if workspace := cfg.WorkspaceID(key, req.Attributes["workspace_id"]); workspace != "" {
		headers.Set("x-org-id", workspace)
	}
	base := cfg.ConsoleURL
	if base == "" {
		base = config.DefaultConsoleURL
	}
	type part struct {
		name    string
		groups  []pluginapi.QuotaGroup
		summary []pluginapi.QuotaMetric
		err     error
	}
	results := make(chan part, 2)
	for _, source := range []struct{ name, path string }{
		{"budgets", "/api/policies/current-budgets"},
		{"billing", "/api/billing/status"},
	} {
		go func() {
			p := part{name: source.name}
			resp, err := m.bridge.Do(ctx, pluginapi.HTTPRequest{Method: http.MethodGet, URL: catalog.JoinUpstreamURL(base, source.path), Headers: headers.Clone()})
			switch {
			case err != nil:
				p.err = fmt.Errorf("%s request failed", source.name)
			case resp.StatusCode == http.StatusForbidden:
				p.err = fmt.Errorf("%s read permission unavailable", source.name)
			case resp.StatusCode == http.StatusUnauthorized:
				p.err = fmt.Errorf("%s credential rejected", source.name)
			case resp.StatusCode != http.StatusOK:
				p.err = fmt.Errorf("%s upstream returned HTTP %d", source.name, resp.StatusCode)
			case len(resp.Body) > quotaResponseLimit:
				p.err = fmt.Errorf("%s response exceeds size limit", source.name)
			case source.name == "budgets":
				p.groups, p.err = normalizeBudgets(resp.Body)
			default:
				p.summary, p.err = normalizeBilling(resp.Body)
			}
			results <- p
		}()
	}
	out := pluginapi.QuotaFetchResponse{Subscription: &pluginapi.QuotaSubscription{Plan: "Pay-as-you-go"}}
	var errorsBySource = make(map[string]error)
	valid := 0
	for range 2 {
		p := <-results
		if p.err != nil {
			errorsBySource[p.name] = p.err
			continue
		}
		valid++
		if p.summary != nil {
			out.Summary = p.summary
		}
		// Billing finishes independently of budgets and must not erase them.
		if p.name != "budgets" {
			continue
		}
		out.Groups = p.groups
	}
	if valid == 0 {
		return empty, fmt.Errorf("quota unavailable: %v; %v", errorsBySource["budgets"], errorsBySource["billing"])
	}
	for _, name := range []string{"budgets", "billing"} {
		if err := errorsBySource[name]; err != nil {
			_ = m.bridge.Log("warn", "OpenCode quota source unavailable", map[string]any{"source": name, "reason": err.Error()})
		}
	}
	return out, nil
}

func normalizeBudgets(body []byte) ([]pluginapi.QuotaGroup, error) {
	var budgets []quotaBudget
	if json.Unmarshal(body, &budgets) != nil || budgets == nil {
		return nil, fmt.Errorf("budgets response invalid")
	}
	if len(budgets) > 128 {
		return nil, fmt.Errorf("too many budget rules")
	}
	groups := make([]pluginapi.QuotaGroup, 0, len(budgets))
	for _, b := range budgets {
		if b.ID == "" || len(b.ID) > 256 || !validMoney(b.Limit) || !validMoney(b.Spent) ||
			(b.Scope != "each" && b.Scope != "combined") || (b.Per != "set" && b.Per != "model") ||
			(b.Coverage != "all" && b.Coverage != "models") || len(b.Models) > 512 {
			return nil, fmt.Errorf("budget rule invalid")
		}
		reset, err := time.Parse(time.RFC3339Nano, b.ResetsAt)
		if err != nil {
			return nil, fmt.Errorf("budget reset time invalid")
		}
		description := fmt.Sprintf("$%.2f limit; scope: %s; coverage: %s", *b.Limit, b.Scope, b.Coverage)
		if b.Shared {
			description += "; shared workspace budget"
		}
		if len(b.Vendors) > 0 {
			vendors := make([]string, 0, len(b.Vendors))
			for _, v := range b.Vendors {
				vendors = append(vendors, v.Name)
			}
			description += "; vendors: " + strings.Join(vendors, ", ")
		}
		if b.InputPriceAtLeast != nil {
			if !validMoney(b.InputPriceAtLeast) {
				return nil, fmt.Errorf("budget price threshold invalid")
			}
			description += fmt.Sprintf("; input price >= $%g/1M tokens", *b.InputPriceAtLeast)
		}
		group := pluginapi.QuotaGroup{DisplayName: "Budget " + b.ID}
		bucket := func(window string, spent float64) pluginapi.QuotaBucket {
			fraction := 0.0
			if *b.Limit > 0 {
				fraction = math.Max(0, math.Min(1, 1 - spent / *b.Limit))
			}
			return pluginapi.QuotaBucket{Window: window, RemainingFraction: fraction, ResetTime: reset.UTC().Format(time.RFC3339Nano), Description: boundedLabel(fmt.Sprintf("$%.2f spent; %s", spent, description), 1024)}
		}
		if b.Per == "model" {
			for _, model := range b.Models {
				if model.Spent == nil {
					continue
				}
				if !validMoney(model.Spent) || model.ModelID == "" {
					return nil, fmt.Errorf("model budget usage invalid")
				}
				group.Buckets = append(group.Buckets, bucket("monthly: "+boundedLabel(model.ProviderID+"/"+model.ModelID, 256), *model.Spent))
			}
			if len(group.Buckets) == 0 {
				group.Buckets = append(group.Buckets, bucket("monthly: most-used model", *b.Spent))
			}
		} else {
			models := make([]string, 0, len(b.Models))
			for _, model := range b.Models {
				models = append(models, model.ProviderID+"/"+model.ModelID)
			}
			if len(models) > 0 {
				description += "; models: " + strings.Join(models, ", ")
			}
			group.Buckets = append(group.Buckets, bucket("monthly", *b.Spent))
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func validMoney(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0 && *value <= 1e15
}

func boundedLabel(s string, limit int) string {
	runes := []rune(s)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func normalizeBilling(body []byte) ([]pluginapi.QuotaMetric, error) {
	var billing struct {
		BillingMode string  `json:"billingMode"`
		Mode        string  `json:"mode"`
		Balance     *string `json:"balanceMicroCents"`
		Available   *string `json:"availableMicroCents"`
		CreditLimit *string `json:"creditLimitMicroCents"`
	}
	if json.Unmarshal(body, &billing) != nil || billing.Balance == nil || billing.Available == nil ||
		(billing.BillingMode != "prepaid" && billing.BillingMode != "credit" && billing.BillingMode != "seat" && billing.BillingMode != "legacy") ||
		(billing.Mode != "pay-as-you-go" && billing.Mode != "invoiceable") {
		return nil, fmt.Errorf("billing response invalid")
	}
	metrics := []pluginapi.QuotaMetric{}
	for _, field := range []struct {
		key, label string
		value      *string
	}{
		{"balance", "Balance", billing.Balance},
		{"available", "Available credits", billing.Available},
		{"credit_limit", "Credit limit", billing.CreditLimit},
	} {
		if field.value == nil {
			continue
		}
		text := *field.value
		if text == "" || len(text) > 32 || strings.Trim(text, "-0123456789") != "" {
			return nil, fmt.Errorf("billing amount invalid")
		}
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, fmt.Errorf("billing amount invalid")
		}
		value, _ := new(big.Rat).SetFrac(integer, big.NewInt(100_000_000)).Float64()
		if math.IsInf(value, 0) || math.IsNaN(value) || math.Abs(value) > 1e15 {
			return nil, fmt.Errorf("billing amount out of range")
		}
		metrics = append(metrics, pluginapi.QuotaMetric{Key: field.key, Label: field.label, Value: value, Format: "currency", Currency: "USD"})
	}
	return metrics, nil
}
