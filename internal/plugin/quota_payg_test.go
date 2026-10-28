package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dillonzq/cpa-opencode/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const budgetFixture = `[{"ruleID":"member-budget","limitDollars":20,"spentDollars":5,"scope":"each","per":"set","coverage":"models","models":[{"providerID":"opencode","modelID":"gpt-test","name":"GPT"}],"vendors":[],"shared":false,"resetsAt":"2026-11-01T08:00:00+08:00"}]`
const billingFixture = `{"billingMode":"prepaid","mode":"pay-as-you-go","balanceMicroCents":"1234567890","availableMicroCents":"2234567890","creditLimitMicroCents":null}`

func TestPAYGQuotaCredentialIsolationAndFreshness(t *testing.T) {
	var mu sync.Mutex
	counts := map[string]int{}
	f := &fakeCaller{responder: func(method string, payload []byte) ([]byte, error) {
		if method != pluginabi.MethodHostHTTPDo {
			return hostOK(struct{}{}), nil
		}
		var req pluginapi.HTTPRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
		key := strings.TrimPrefix(req.Headers.Get("Authorization"), "Bearer ")
		if (key != "first" && key != "second") || req.Headers.Get("x-org-id") != "workspace-"+key {
			return hostErr("test", "credential/workspace mismatch"), nil
		}
		mu.Lock()
		counts[key]++
		mu.Unlock()
		body := budgetFixture
		if strings.HasSuffix(req.URL, "/api/billing/status") {
			balance := "100000000"
			if key == "second" {
				balance = "200000000"
			}
			body = fmt.Sprintf(`{"billingMode":"prepaid","mode":"pay-as-you-go","balanceMicroCents":%q,"availableMicroCents":%q}`, balance, balance)
		} else if req.URL != "https://quota.test/console/api/policies/current-budgets" {
			return hostErr("test", "wrong quota URL"), nil
		}
		return hostOK(pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(body)}), nil
	}}
	m := NewManager(NewHostBridge(f.call))
	m.cfg = config.Config{ConsoleURL: "https://quota.test/console", Credentials: []config.Credential{{Value: "first", WorkspaceID: "workspace-first"}, {Value: "second", WorkspaceID: "workspace-second"}}}
	var wg sync.WaitGroup
	for _, key := range []string{"first", "second", "first", "second"} {
		wg.Go(func() {
			got, err := m.FetchQuota(context.Background(), pluginapi.QuotaFetchRequest{Provider: ProviderID, AuthIndex: key, Attributes: map[string]string{"api_key": key, "workspace_id": "stale-workspace"}})
			if err != nil {
				t.Error(err)
				return
			}
			want := 1.0
			if key == "second" {
				want = 2
			}
			if got.Subscription.Plan != "Pay-as-you-go" || len(got.Groups) != 1 || len(got.Summary) != 2 || got.Summary[0].Value != want || got.Groups[0].Buckets[0].RemainingFraction != 0.75 {
				t.Errorf("crossed or missing quota data for %s: %+v", key, got)
			}
		})
	}
	wg.Wait()
	if counts["first"] != 4 || counts["second"] != 4 {
		t.Fatalf("queries reused stale quota: %v", counts)
	}
	for _, req := range []pluginapi.QuotaFetchRequest{{Provider: "other"}, {Provider: ProviderID}, {Provider: ProviderID, Attributes: map[string]string{"api_key": "removed"}}} {
		if _, err := m.FetchQuota(context.Background(), req); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
}

func TestPAYGQuotaPartialAndPermissionResults(t *testing.T) {
	for _, tc := range []struct {
		name, budgets, billing string
		bs, cs                 int
		groups, summary        int
		wantError              bool
	}{
		{"all", budgetFixture, billingFixture, 200, 200, 1, 2, false},
		{"empty budgets", "[]", billingFixture, 200, 200, 0, 2, false},
		{"no configured budgets", "[]", "", 200, 403, 0, 0, false},
		{"billing denied", budgetFixture, "secret", 200, 403, 1, 0, false},
		{"service account budgets denied", "secret", billingFixture, 403, 200, 0, 2, false},
		{"both denied", "secret", "secret", 403, 403, 0, 0, true},
		{"both malformed", "{}", "secret", 200, 200, 0, 0, true},
		{"both unauthorized", "secret", "secret", 401, 401, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeCaller{responder: func(method string, payload []byte) ([]byte, error) {
				if method != pluginabi.MethodHostHTTPDo {
					return hostOK(struct{}{}), nil
				}
				var req pluginapi.HTTPRequest
				json.Unmarshal(payload, &req)
				status, body := tc.bs, tc.budgets
				if strings.HasSuffix(req.URL, "/billing/status") {
					status, body = tc.cs, tc.billing
				}
				return hostOK(pluginapi.HTTPResponse{StatusCode: status, Body: []byte(body)}), nil
			}}
			m := NewManager(NewHostBridge(f.call))
			got, err := m.FetchQuota(context.Background(), pluginapi.QuotaFetchRequest{Provider: ProviderID, Attributes: map[string]string{"api_key": "secret"}})
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected quota result: %+v %v", got, err)
			}
			if err != nil {
				if strings.Contains(err.Error(), "secret") {
					t.Fatal("quota error leaked credential")
				}
				return
			}
			if len(got.Groups) != tc.groups || len(got.Summary) != tc.summary {
				t.Fatalf("partial data lost: %+v", got)
			}
		})
	}
}

func TestPAYGBudgetNormalization(t *testing.T) {
	groups, err := normalizeBudgets([]byte(budgetFixture))
	if err != nil || groups[0].Buckets[0].ResetTime != "2026-11-01T00:00:00Z" || !strings.Contains(groups[0].Buckets[0].Description, "opencode/gpt-test") {
		t.Fatalf("budget mapping: %+v %v", groups, err)
	}
	for _, tt := range []struct {
		spent, limit string
		want         float64
	}{{"0", "0", 0}, {"50", "20", 0}, {"0", "20", 1}, {"2.5", "20", .875}} {
		body := strings.ReplaceAll(strings.ReplaceAll(budgetFixture, `"limitDollars":20`, `"limitDollars":`+tt.limit), `"spentDollars":5`, `"spentDollars":`+tt.spent)
		got, err := normalizeBudgets([]byte(body))
		if err != nil || got[0].Buckets[0].RemainingFraction != tt.want {
			t.Fatalf("fraction %+v %v", got, err)
		}
	}
	perModel := `[{"ruleID":"shared-models","limitDollars":10,"spentDollars":8,"scope":"combined","per":"model","coverage":"models","shared":true,"models":[{"providerID":"opencode","modelID":"a","spentDollars":8},{"providerID":"opencode","modelID":"b","spentDollars":2}],"resetsAt":"2026-11-01T00:00:00Z"}]`
	groups, err = normalizeBudgets([]byte(perModel))
	if err != nil || len(groups[0].Buckets) != 2 || !strings.Contains(groups[0].Buckets[0].Description, "shared") || groups[0].Buckets[1].RemainingFraction != .8 {
		t.Fatalf("model budgets merged: %+v %v", groups, err)
	}
	for _, body := range []string{"null", "{}", "not JSON", strings.ReplaceAll(budgetFixture, `"limitDollars":20`, `"limitDollars":null`), strings.ReplaceAll(budgetFixture, `"spentDollars":5`, `"spentDollars":-1`), strings.ReplaceAll(budgetFixture, "2026-11-01T08:00:00+08:00", "invalid"), strings.ReplaceAll(budgetFixture, `"per":"set"`, `"per":"unknown"`)} {
		if _, err := normalizeBudgets([]byte(body)); err == nil {
			t.Fatalf("invalid budget accepted: %s", body)
		}
	}
}

func TestPAYGBillingMoneyPrecision(t *testing.T) {
	got, err := normalizeBilling([]byte(billingFixture))
	if err != nil || len(got) != 2 || got[0].Value != 12.34567890 || got[1].Value != 22.34567890 {
		t.Fatalf("money mapping: %+v %v", got, err)
	}
	if got[0].Format != "currency" || got[0].Currency != "USD" {
		t.Fatal("money published as percentage")
	}
	credit := strings.ReplaceAll(strings.ReplaceAll(billingFixture, `"billingMode":"prepaid"`, `"billingMode":"credit"`), `"creditLimitMicroCents":null`, `"creditLimitMicroCents":"10000000000"`)
	got, err = normalizeBilling([]byte(credit))
	if err != nil || len(got) != 3 || got[2].Value != 100 {
		t.Fatalf("credit mapping: %+v %v", got, err)
	}
	zero := strings.ReplaceAll(strings.ReplaceAll(billingFixture, "1234567890", "0"), "2234567890", "0")
	got, err = normalizeBilling([]byte(zero))
	if err != nil || got[0].Value != 0 || got[1].Value != 0 {
		t.Fatal("zero balance lost")
	}
	for _, body := range []string{"{}", "null", strings.ReplaceAll(billingFixture, `"1234567890"`, `null`), strings.ReplaceAll(billingFixture, `"1234567890"`, `"1e8"`), strings.ReplaceAll(billingFixture, `"1234567890"`, `"99999999999999999999999999999999"`), strings.ReplaceAll(billingFixture, `"1234567890"`, `"NaN"`)} {
		if _, err := normalizeBilling([]byte(body)); err == nil {
			t.Fatalf("invalid billing accepted: %s", body)
		}
	}
}

func TestPAYGAuthRefreshPreservesState(t *testing.T) {
	cfg := config.Config{Credentials: []config.Credential{{Name: "Work", Value: "key", WorkspaceID: "new-workspace"}}}
	attrs := map[string]string{"api_key": "key", "workspace_id": "old-workspace", "proxy_url": "preserve"}
	metadata := map[string]any{"disabled": true, "custom": "preserve"}
	p := authProvider{cfg: cfg}
	got, err := p.RefreshAuth(context.Background(), pluginapi.AuthRefreshRequest{AuthID: "stable", AuthProvider: ProviderID, StorageJSON: []byte(`{"disabled":true}`), Attributes: attrs, Metadata: metadata})
	if err != nil || got.Auth.ID != "stable" || got.Auth.Attributes["workspace_id"] != "new-workspace" || !reflect.DeepEqual(got.Auth.Metadata, metadata) || attrs["workspace_id"] != "old-workspace" {
		t.Fatalf("refresh mutated state: %+v %v", got, err)
	}
	if _, err := p.RefreshAuth(context.Background(), pluginapi.AuthRefreshRequest{AuthProvider: ProviderID, Attributes: map[string]string{"api_key": "removed"}}); err == nil {
		t.Fatal("removed key refreshed")
	}
	if accountLabel(cfg, "key", "Other", 0) != "Work" || accountLabel(config.Config{}, "key", "Existing label", 0) != "Existing label" {
		t.Fatal("wrong display label")
	}
}

func TestPAYGQuotaTimeoutCancelsBothOperations(t *testing.T) {
	canceled := make(chan string, 2)
	blocked := make(chan struct{})
	defer close(blocked)
	f := &fakeCaller{responder: func(method string, payload []byte) ([]byte, error) {
		if method == pluginabi.MethodHostHTTPDo {
			<-blocked
			return hostOK(pluginapi.HTTPResponse{StatusCode: http.StatusOK}), nil
		}
		if method == pluginabi.MethodHostHTTPCancel {
			var req httpOperationRequest
			json.Unmarshal(payload, &req)
			canceled <- req.OperationID
		}
		return hostOK(struct{}{}), nil
	}}
	m := NewManager(NewHostBridge(f.call))
	m.cfg.QuotaTimeout = 20 * time.Millisecond
	_, err := m.FetchQuota(context.Background(), pluginapi.QuotaFetchRequest{Provider: ProviderID, Attributes: map[string]string{"api_key": "key"}})
	if err == nil {
		t.Fatal("timeout succeeded")
	}
	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-canceled:
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatal("quota operation was not canceled")
		}
	}
	if len(seen) != 2 {
		t.Fatalf("canceled operations: %v", seen)
	}
}
