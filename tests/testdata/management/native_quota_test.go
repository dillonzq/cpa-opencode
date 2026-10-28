//go:build debug && cgo && (darwin || linux)

package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	"gopkg.in/yaml.v3"
)

// Load the real shared library through public host discovery and exercise the
// actual v8 management handlers and core credential scheduler end to end.
func TestOpenCodeNativePAYGQuotaAndCooldown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var funds atomic.Bool
	var mu sync.Mutex
	var executed []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			if r.Header.Get("Authorization") != "" {
				t.Error("public catalog received credential")
			}
			io.WriteString(w, `{"data":[{"id":"glm-5.3"}]}`)
			return
		}
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key != "offline-a" && key != "offline-b" {
			http.Error(w, "wrong selected key", 401)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/console/api/") {
			if r.Header.Get("x-org-id") != "workspace-"+key {
				http.Error(w, "wrong workspace", 400)
				return
			}
			switch r.URL.Path {
			case "/console/api/policies/current-budgets":
				fmt.Fprintf(w, `[{"ruleID":%q,"limitDollars":20,"spentDollars":5,"scope":"each","per":"set","coverage":"all","models":[],"vendors":[],"resetsAt":"2026-11-01T00:00:00Z"}]`, key)
			case "/console/api/billing/status":
				balance := "100000000"
				if key == "offline-b" {
					balance = "200000000"
				}
				if key == "offline-a" && funds.Load() {
					balance = "1000000000"
				}
				fmt.Fprintf(w, `{"billingMode":"prepaid","mode":"pay-as-you-go","balanceMicroCents":%q,"availableMicroCents":%q}`, balance, balance)
			default:
				http.NotFound(w, r)
			}
			return
		}
		if r.URL.Path != "/openai/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		executed = append(executed, key)
		mu.Unlock()
		if key == "offline-a" && !funds.Load() {
			w.WriteHeader(http.StatusPaymentRequired)
			io.WriteString(w, `{"error":{"message":"Insufficient account funds"}}`)
			return
		}
		io.WriteString(w, `{"id":"native-payg","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()
	pluginDir := t.TempDir()
	binary, err := os.ReadFile(os.Getenv("CPA_NATIVE_PLUGIN"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "cpa-opencode"+pluginhost.PluginExtension(runtime.GOOS)), binary, 0600); err != nil {
		t.Fatal(err)
	}
	var item config.PluginInstanceConfig
	settings := fmt.Sprintf(`enabled: true
credentials:
  - name: A
    api-key: offline-a
    workspace-id: workspace-offline-a
  - name: B
    api-key: offline-b
    workspace-id: workspace-offline-b
base-url: %s
console-url: %s/console
allow-http: true
models-dev:
  enabled: false
`, upstream.URL, upstream.URL)
	if err := yaml.Unmarshal([]byte(settings), &item); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AuthDir: t.TempDir(), Plugins: config.PluginsConfig{Enabled: true, Dir: pluginDir, Configs: map[string]config.PluginInstanceConfig{"cpa-opencode": item}}}
	host := pluginhost.New()
	host.ApplyConfig(ctx, cfg)
	defer host.ShutdownAll()
	if !host.PluginLoaded("cpa-opencode") {
		t.Fatal("native PAYG plugin was not loaded")
	}
	providers := host.QuotaProviders(ctx)
	if len(providers) != 1 || providers[0].Provider != "opencode" || providers[0].SupportsReset {
		t.Fatalf("quota provider registration: %+v", providers)
	}
	manager := coreauth.NewManager(nil, &coreauth.FillFirstSelector{}, nil)
	manager.SetRetryConfig(0, 0, 2)
	host.SetAuthManager(manager)
	host.RegisterModels(ctx, registry.GetGlobalRegistry())
	host.RegisterExecutors(manager, registry.GetGlobalRegistry())
	auths := map[string]*coreauth.Auth{}
	for _, name := range []string{"A", "B"} {
		path := filepath.Join(cfg.AuthDir, name+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		auth, handled, err := host.ParseAuth(ctx, pluginapi.AuthParseRequest{Provider: "opencode", Path: path, FileName: name + ".json", RawJSON: raw})
		if err != nil || !handled {
			t.Fatalf("parse %s: %v", name, err)
		}
		if name == "A" {
			auth.Attributes["priority"] = "10"
		}
		auth, err = manager.Register(ctx, auth)
		if err != nil {
			t.Fatal(err)
		}
		auths[name] = auth
		registry.GetGlobalRegistry().RegisterClient(auth.ID, "opencode", []*registry.ModelInfo{{ID: "opencode/glm-5.3", Object: "model", OwnedBy: "opencode"}})
		defer registry.GetGlobalRegistry().UnregisterClient(auth.ID)
	}
	if auths["A"].ID == auths["B"].ID || auths["A"].Index == auths["B"].Index {
		t.Fatal("credentials share identity")
	}
	payload := []byte(`{"model":"opencode/glm-5.3","messages":[{"role":"user","content":"offline quota test"}]}`)
	execute := func() {
		t.Helper()
		got, err := manager.Execute(ctx, []string{"opencode"}, coreexecutor.Request{Model: "opencode/glm-5.3", Payload: payload}, coreexecutor.Options{SourceFormat: translator.FormatOpenAI, OriginalRequest: payload})
		if err != nil || !strings.Contains(string(got.Payload), "ok") {
			t.Fatalf("native execute: %s %v", got.Payload, err)
		}
	}
	execute()
	mu.Lock()
	seen := append([]string(nil), executed...)
	mu.Unlock()
	if !reflect.DeepEqual(seen, []string{"offline-a", "offline-b"}) {
		t.Fatalf("402 did not switch credential: %v", seen)
	}
	isCooled := func(a *coreauth.Auth) bool {
		if a.Unavailable || a.Quota.Exceeded || !a.NextRetryAfter.IsZero() {
			return true
		}
		for _, s := range a.ModelStates {
			if s != nil && (s.Unavailable || s.Quota.Exceeded || !s.NextRetryAfter.IsZero()) {
				return true
			}
		}
		return false
	}
	aBefore, _ := manager.GetByID(auths["A"].ID)
	if !isCooled(aBefore) {
		t.Fatal("402 did not enter CPA cooldown")
	}
	manager.MarkResult(ctx, coreauth.Result{AuthID: auths["B"].ID, Provider: "opencode", Model: "opencode/glm-5.3", Success: false, Error: &coreauth.Error{HTTPStatus: 429, Message: "rate limited"}})
	bBefore, _ := manager.GetByID(auths["B"].ID)
	if !isCooled(bBefore) {
		t.Fatal("429 did not enter independent cooldown")
	}
	h := NewHandlerWithoutConfigFilePath(cfg, manager)
	h.SetPluginHost(host)
	router := gin.New()
	router.POST("/v8/management/credentials/quota/fetch", h.FetchCredentialQuota)
	router.POST("/v8/management/routing/cooldown/reset", h.ResetQuota)
	router.GET("/v8/management/plugins/:id/quota", h.GetPluginQuota)
	post := func(path string, auth *coreauth.Auth) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(fmt.Sprintf(`{"auth_index":%q}`, auth.Index))).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		return rec
	}
	for _, name := range []string{"A", "B"} {
		rec := post("/v8/management/credentials/quota/fetch", auths[name])
		var quota pluginapi.QuotaFetchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &quota); err != nil {
			t.Fatal(err)
		}
		want := 1.0
		if name == "B" {
			want = 2
		}
		if len(quota.Groups) != 1 || len(quota.Summary) != 2 || quota.Summary[0].Value != want || quota.Groups[0].DisplayName != "Budget offline-"+strings.ToLower(name) {
			t.Fatalf("wrong selected quota: %+v", quota)
		}
	}
	aAfter, _ := manager.GetByID(auths["A"].ID)
	bAfter, _ := manager.GetByID(auths["B"].ID)
	if !reflect.DeepEqual(aBefore.ModelStates, aAfter.ModelStates) || !reflect.DeepEqual(bBefore.ModelStates, bAfter.ModelStates) {
		t.Fatal("quota refresh mutated cooldown")
	}
	post("/v8/management/routing/cooldown/reset", auths["A"])
	aAfter, _ = manager.GetByID(auths["A"].ID)
	bAfter, _ = manager.GetByID(auths["B"].ID)
	if isCooled(aAfter) || !reflect.DeepEqual(bBefore.ModelStates, bAfter.ModelStates) {
		t.Fatal("cooldown reset affected wrong credential")
	}
	funds.Store(true)
	execute()
	mu.Lock()
	last := executed[len(executed)-1]
	mu.Unlock()
	if last != "offline-a" {
		t.Fatal("reset credential was not restored to routing")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/v8/management/plugins/cpa-opencode/quota?auth_index="+auths["A"].Index, nil).WithContext(ctx))
	var refreshed pluginapi.QuotaFetchResponse
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &refreshed) != nil || len(refreshed.Summary) != 2 || refreshed.Summary[0].Value != 10 {
		t.Fatalf("quota did not refresh after top-up: %s", rec.Body.String())
	}
}
