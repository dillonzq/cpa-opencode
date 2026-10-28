package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"github.com/dillonzq/cpa-opencode/internal/catalog"
	"github.com/dillonzq/cpa-opencode/internal/config"
	"github.com/dillonzq/cpa-opencode/internal/errclass"
)

// ProviderID is the single provider key served by this plugin.
const ProviderID = "opencode"

// pluginName is the identifier used for configuration and management routes.
const pluginName = "cpa-opencode"

// githubRepoURL satisfies the host's validPlugin gate (host.go
// validPlugin rejects empty Metadata.GitHubRepository).
const githubRepoURL = "https://github.com/dillonzq/cpa-opencode"

// pluginVersion is set from the release tag using -ldflags -X.
var pluginVersion = "0.0.0-dev"

// registerRefreshTimeout bounds ONLY the synchronous initial/reconfigure
// refreshOnce so a slow catalog cannot block host startup/reconfigure for a
// full request-timeout (default 15m). On expiry registration proceeds per
// empty/stale semantics; the ticker retries at refresh-interval with
// the full request-timeout.
const registerRefreshTimeout = 10 * time.Second

// Manager owns dispatcher state (config snapshot, catalog manager, refresh
// loop) and routes every RPC method. Safe for concurrent HandleCall use.
type Manager struct {
	bridge *HostBridge // immutable after NewManager

	// lifeMu serializes whole register/reconfigure/shutdown sequences so
	// their stop-wait-install steps cannot interleave into orphaned tickers.
	lifeMu     sync.Mutex
	drainMu    sync.Mutex
	workMu     sync.Mutex
	work       sync.WaitGroup
	quiescing  bool
	workCtx    context.Context
	cancelWork context.CancelFunc

	mu  sync.RWMutex
	cfg config.Config
	mgr *catalog.Manager
	// stop/done manage the one background refresh goroutine; both nil
	// when no loop is running.
	stop chan struct{}
	done chan struct{}
}

// NewManager returns a dispatcher whose outbound traffic flows through bridge.
func NewManager(bridge *HostBridge) *Manager {
	return &Manager{bridge: bridge}
}

// HandleCall dispatches one RPC method and returns envelope bytes. Handler
// failures travel inside the envelope; a recovered panic becomes a
// "plugin_error" envelope so the host process never dies with us.
func (m *Manager) HandleCall(method string, request []byte) (resp []byte, err error) {
	debugTrace("handle method=%s request_bytes=%d", method, len(request))
	defer func() {
		// Defensive: host-boundary panics are goroutine-contained (see
		// HostBridge.callWithTimeout); this guards future handler bugs.
		if r := recover(); r != nil {
			resp = ErrEnvelope("plugin_error", fmt.Sprintf("internal error handling %s", method))
			err = nil
		}
	}()
	if method != pluginabi.MethodPluginShutdown && method != pluginabi.MethodPluginQuiesce {
		m.workMu.Lock()
		if m.quiescing {
			m.workMu.Unlock()
			return ErrEnvelope("plugin_quiescing", "plugin is stopping"), nil
		}
		m.initWorkContextLocked()
		m.work.Add(1)
		m.workMu.Unlock()
		defer m.work.Done()
	}
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return m.handleLifecycle(request)
	case pluginabi.MethodModelStatic:
		return m.handleModels()
	case pluginabi.MethodModelForAuth:
		var req pluginapi.AuthModelRequest
		if len(request) > 0 && json.Unmarshal(request, &req) != nil {
			return ErrEnvelope("invalid_request", "malformed model request"), nil
		}
		m.mu.RLock()
		active := m.cfg.HasCredential(strings.TrimSpace(req.Attributes["api_key"]))
		m.mu.RUnlock()
		if req.AuthProvider != "" && (req.AuthProvider != ProviderID || !active) {
			return okEnvelope(pluginapi.ModelResponse{Provider: ProviderID, Models: []pluginapi.ModelInfo{}}), nil
		}
		return m.handleModels()
	case pluginabi.MethodPluginQuiesce:
		return m.handleQuiesce(false)
	case pluginabi.MethodPluginShutdown:
		return m.handleShutdown()
	case pluginabi.MethodExecutorExecute:
		return m.handleExecute(request)
	case pluginabi.MethodExecutorExecuteStream:
		return m.handleExecuteStream(request)
	case pluginabi.MethodAuthIdentifier:
		return okEnvelope(map[string]string{"identifier": ProviderID}), nil
	case pluginabi.MethodAuthParse:
		var req pluginapi.AuthParseRequest
		if json.Unmarshal(request, &req) != nil {
			return ErrEnvelope("invalid_request", "malformed auth parse request body"), nil
		}
		m.mu.RLock()
		cfg := m.cfg
		m.mu.RUnlock()
		resp, err := (authProvider{cfg: cfg}).ParseAuth(context.Background(), req)
		if err != nil {
			return ErrEnvelope("auth_failure", err.Error()), nil
		}
		debugTrace("auth parse response provider=%s id=%s attr_api_key_present=%t storage_json_bytes=%d", resp.Auth.Provider, resp.Auth.ID, strings.TrimSpace(resp.Auth.Attributes["api_key"]) != "", len(resp.Auth.StorageJSON))
		return okEnvelope(resp), nil
	case pluginabi.MethodAuthLoginStart:
		var req struct {
			pluginapi.AuthLoginStartRequest
		}
		if json.Unmarshal(request, &req) != nil {
			return ErrEnvelope("invalid_request", "malformed auth login request body"), nil
		}
		_, err := (authProvider{}).StartLogin(context.Background(), req.AuthLoginStartRequest)
		return ErrEnvelope("unsupported", err.Error()), nil
	case pluginabi.MethodAuthLoginPoll:
		var req struct{ pluginapi.AuthLoginPollRequest }
		if json.Unmarshal(request, &req) != nil {
			return ErrEnvelope("invalid_request", "malformed auth poll request body"), nil
		}
		_, err := (authProvider{}).PollLogin(context.Background(), req.AuthLoginPollRequest)
		return ErrEnvelope("unsupported", err.Error()), nil
	case pluginabi.MethodAuthRefresh:
		var req struct{ pluginapi.AuthRefreshRequest }
		if json.Unmarshal(request, &req) != nil {
			return ErrEnvelope("invalid_request", "malformed auth refresh request body"), nil
		}
		m.mu.RLock()
		cfg := m.cfg
		m.mu.RUnlock()
		resp, err := (authProvider{cfg: cfg}).RefreshAuth(context.Background(), req.AuthRefreshRequest)
		if err != nil {
			return ErrEnvelope("auth_failure", err.Error()), nil
		}
		return okEnvelope(resp), nil
	case pluginabi.MethodQuotaIdentifier:
		return okEnvelope(map[string]string{"identifier": ProviderID}), nil
	case pluginabi.MethodQuotaDescribe:
		return okEnvelope(pluginapi.QuotaDescribeResponse{SupportedProviders: []string{ProviderID}, DisplayName: "OpenCode Pay-as-you-go"}), nil
	case pluginabi.MethodQuotaFetch:
		var req struct {
			pluginapi.QuotaFetchRequest
			HostCallbackID string `json:"host_callback_id,omitempty"`
		}
		if json.Unmarshal(request, &req) != nil {
			return ErrEnvelope("invalid_request", "malformed quota request"), nil
		}
		result, err := m.FetchQuota(withHostCallbackScope(m.workContext(), req.HostCallbackID), req.QuotaFetchRequest)
		if err != nil {
			return ErrEnvelope("quota_failure", err.Error()), nil
		}
		return okEnvelope(result), nil
	case pluginabi.MethodQuotaReset:
		return ErrEnvelope("unsupported", "OpenCode billing cannot be reset; use CPA routing/cooldown/reset to clear local cooldown"), nil
	case pluginabi.MethodExecutorIdentifier:
		return okEnvelope(map[string]string{"identifier": ProviderID}), nil
	case pluginabi.MethodExecutorCountTokens:
		return classEnvelope(&errclass.Error{
			Class:   errclass.ClassUnsupported,
			Message: "executor.count_tokens has no OpenCode equivalent",
		}), nil
	case pluginabi.MethodExecutorHTTPRequest:
		return classEnvelope(&errclass.Error{
			Class:   errclass.ClassUnsupported,
			Message: fmt.Sprintf("%s endpoint is not supported by opencode", pluginabi.MethodExecutorHTTPRequest),
		}), nil
	default:
		return ErrEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

// lifecycleRequest mirrors rpcLifecycleRequest: config_yaml is base64.
// The request's schema_version is decoded-and-ignored; registration echoes
// pluginabi.SchemaVersion.
type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type capabilities struct {
	ModelProvider         bool                         `json:"model_provider"`
	AuthProvider          bool                         `json:"auth_provider"`
	Executor              bool                         `json:"executor"`
	ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope,omitempty"`
	ExecutorInputFormats  []string                     `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats []string                     `json:"executor_output_formats,omitempty"`
	QuotaProvider         bool                         `json:"quota_provider"`
	ManagementAPI         bool                         `json:"management_api"`
}

type registrationResult struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  capabilities       `json:"capabilities"`
}

// pluginConfigFields describes settings that management clients can discover
// through the registration metadata.
func pluginConfigFields() []pluginapi.ConfigField {
	return []pluginapi.ConfigField{
		{Name: "models", Type: pluginapi.ConfigFieldTypeArray, Description: "Model declarations and metadata overrides using CPA provider fields: name, display-name, max-context-length, input-modalities, output-modalities, thinking; plus description, max-tokens, and protocols (`chat-completions|messages|responses|gemini: {enabled, endpoint}`), which switches protocols on or off on top of the built-in set and overrides endpoints. New IDs use family routing or protocols."},
		{Name: "models-dev", Type: pluginapi.ConfigFieldTypeObject, Description: "Fallback metadata source (enabled, url, refresh-interval); priority: models > catalog > models.dev."},
		{
			Name:        "credentials",
			Type:        pluginapi.ConfigFieldTypeArray,
			Description: "OpenCode credentials: api-key, optional name and workspace-id. Supports ${ENV_VAR} expansion in api-key only.",
		},
		{
			Name:        "base-url",
			Type:        pluginapi.ConfigFieldTypeString,
			Description: "PAYG inference base URL (default: https://opencode.ai/inference). Protocol paths are appended without rewriting the base.",
		},
		{Name: "console-url", Type: pluginapi.ConfigFieldTypeString, Description: "Console API base URL for budgets and billing (default: https://opencode.ai/console)."},
		{Name: "quota", Type: pluginapi.ConfigFieldTypeObject, Description: "Quota lookup settings: timeout (default: 15s, maximum: 1m). Each fetch queries the selected credential afresh."},
		{
			Name:        "catalog-url",
			Type:        pluginapi.ConfigFieldTypeString,
			Description: "Optional catalog discovery URL (default: {base-url}/v1/models).",
		},
		{
			Name:        "model-prefix",
			Type:        pluginapi.ConfigFieldTypeObject,
			Description: "Client-facing model ID prefix configuration (`enabled: bool`, `value: string`).",
		},
		{
			Name:        "catalog",
			Type:        pluginapi.ConfigFieldTypeObject,
			Description: "Catalog discovery settings (`refresh-interval: string`, `stale-while-unavailable: bool`).",
		},
		{
			Name:        "protocols",
			Type:        pluginapi.ConfigFieldTypeObject,
			Description: "Endpoint paths for every model, keyed by protocol (`chat-completions|messages|responses|gemini: {endpoint: /path}`). Switch protocols per model under models[].protocols.",
		},
		{
			Name:        "request-timeout",
			Type:        pluginapi.ConfigFieldTypeString,
			Description: "Upstream HTTP request timeout (default: 5m).",
		},
		{
			Name:        "max-response-bytes",
			Type:        pluginapi.ConfigFieldTypeInteger,
			Description: "Max non-streaming response body size in bytes (default: 67108864).",
		},
		{
			Name:        "allow-http",
			Type:        pluginapi.ConfigFieldTypeBoolean,
			Description: "Allow plain http:// URLs for local testing/mocking (default: false).",
		},
	}
}

func registrationEnvelope() []byte {
	formats := []string{"openai", "claude", "openai-response", "gemini"}
	return okEnvelope(registrationResult{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           "dillonzq",
			GitHubRepository: githubRepoURL,
			Logo:             "https://raw.githubusercontent.com/dillonzq/cpa-opencode/main/assets/logo.png",
			ConfigFields:     pluginConfigFields(),
		},
		Capabilities: capabilities{
			ModelProvider:         true,
			AuthProvider:          true,
			Executor:              true,
			ExecutorModelScope:    pluginapi.ExecutorModelScopeOAuth,
			ExecutorInputFormats:  formats,
			ExecutorOutputFormats: formats,
			QuotaProvider:         true,
		},
	})
}

// handleLifecycle implements plugin.register / plugin.reconfigure: load
// config, refresh the catalog once, publish state, restart the refresh
// loop. Registration succeeds even when the initial refresh fails.
func (m *Manager) handleLifecycle(request []byte) ([]byte, error) {
	var req lifecycleRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return ErrEnvelope("invalid_request", "malformed lifecycle request body"), nil
	}
	cfg, err := config.Load(req.ConfigYAML)
	if err != nil {
		debugTrace("lifecycle config_error=%s", err.Error())
		return ErrEnvelope("invalid_config", err.Error()), nil
	}
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	debugTrace("lifecycle config_loaded key_count=%d prefix_enabled=%t prefix=%s", len(cfg.Credentials), cfg.ModelPrefix.Enabled, cfg.ModelPrefix.Value)
	ctx, cancel := context.WithTimeout(m.workContext(), registerRefreshTimeout)
	defer cancel()
	if err := m.materializeAuthRecords(ctx, cfg); err != nil {
		return ErrEnvelope("auth_materialization_failed", err.Error()), nil
	}
	// A nil *HostBridge must not enter the interface as a typed nil, or
	// catalog's nil-client guard never fires and Refresh panics inside Do.
	var client catalog.HostClient
	if m.bridge != nil {
		client = m.bridge
	}
	mgr := catalog.New(cfg, client)
	m.mu.RLock()
	previous := m.mgr
	m.mu.RUnlock()
	mgr.SeedFallbackFrom(previous)
	refreshErr := refreshOnce(m.workContext(), mgr, m.bridge, registerRefreshTimeout, cfg)
	debugTrace("lifecycle refresh_complete model_count=%d refresh_error=%t", len(mgr.Models()), refreshErr != nil)

	// Retire any running loop and wait for its exit outside m.mu: a mid-refresh
	// tick must never stall readers holding RLock (F4). lifeMu keeps the
	// stop-wait-install sequence atomic against other lifecycles.
	if oldDone := m.closeStop(); oldDone != nil {
		<-oldDone
	}

	m.mu.Lock()
	m.cfg = cfg
	// stale-while-unavailable: a failed refresh must neither wipe a
	// non-empty previous snapshot nor keep the OLD manager, whose stored cfg
	// would pin the old base-url/catalog-url/protocols/prefix so ticks
	// fetched the old URL with the new config's keys forever (F5). The NEW
	// manager is adopted unconditionally; when a previous snapshot exists
	// AND the NEW config keeps stale-while-unavailable enabled, it is
	// seeded into the new one and rebuilt with the NEW config. Carry raw
	// entries even when the old config routed none of them, so the new
	// config can restore them during an outage. Under fail-closed
	// (stale-while-unavailable:false) no seed happens — same as the ticker
	// failure path — so a reconfigure during an outage serves nothing until
	// a refresh succeeds, honoring the operator's policy on BOTH paths.
	if refreshErr != nil && cfg.Catalog.StaleWhileUnavailable && m.mgr != nil {
		mgr.SeedFrom(m.mgr)
	}
	m.mgr = mgr
	stop, done := make(chan struct{}), make(chan struct{})
	m.stop, m.done = stop, done
	interval := cfg.Catalog.RefreshInterval
	m.mu.Unlock()

	m.startRefreshLoop(cfg, mgr, interval, stop, done)
	return registrationEnvelope(), nil
}

// materializeAuthRecords makes CPA-visible auth files idempotently. Existing
// records are discovered through CPA so their host-managed metadata is never
// overwritten. The full key digest is non-secret and independent of config
// ordering. The host ABI has no delete/disable callback, so removed keys remain
// stale records and are not claimed as removed.
func (m *Manager) materializeAuthRecords(ctx context.Context, cfg config.Config) error {
	if m.bridge == nil {
		return nil
	}
	entries, err := m.bridge.AuthList(ctx)
	if err != nil {
		return fmt.Errorf("list existing auth records: %w", err)
	}
	existing := make(map[string]struct{}, len(entries)*2)
	for _, entry := range entries {
		// Before the auth manager is ready, v8 lists disk files without IDs.
		// Recover identity from those host-reported paths to avoid duplicates
		// when a credential has a readable filename.
		if (entry.Provider == ProviderID || entry.Type == ProviderID) && !strings.HasPrefix(entry.ID, "opencode-key-") {
			if !filepath.IsAbs(entry.Path) {
				return fmt.Errorf("existing auth record has no absolute path")
			}
			raw, err := os.ReadFile(entry.Path)
			if err != nil {
				return fmt.Errorf("cannot read existing plugin auth record")
			}
			var record struct {
				APIKey string `json:"api_key"`
			}
			if json.Unmarshal(raw, &record) != nil || record.APIKey == "" {
				return fmt.Errorf("existing plugin auth record is invalid")
			}
			digest := sha256.Sum256([]byte(record.APIKey))
			existing["opencode-key-"+hex.EncodeToString(digest[:])] = struct{}{}
		}
		if name := strings.TrimSpace(entry.Name); name != "" {
			existing[name] = struct{}{}
		}
		if id := strings.TrimSpace(entry.ID); id != "" {
			existing[id] = struct{}{}
		}
	}
	for i, key := range cfg.Credentials {
		digest := sha256.Sum256([]byte(key.Value))
		hash := hex.EncodeToString(digest[:])
		id := "opencode-key-" + hash
		name := id + ".json"
		if _, ok := existing[id]; ok {
			continue
		}
		if _, ok := existing[name]; ok {
			continue
		}
		name = availableAuthName(accountLabel(cfg, key.Value, "", i), existing)
		record, err := json.Marshal(struct {
			Type        string `json:"type"`
			ID          string `json:"id"`
			Label       string `json:"label"`
			APIKey      string `json:"api_key"`
			WorkspaceID string `json:"workspace_id,omitempty"`
		}{
			Type: ProviderID, ID: id, Label: accountLabel(cfg, key.Value, "", i), APIKey: key.Value, WorkspaceID: key.WorkspaceID,
		})
		if err != nil {
			return fmt.Errorf("build auth record")
		}
		if err := m.bridge.AuthSave(ctx, pluginapi.HostAuthSaveRequest{
			Name: name, JSON: record,
		}); err != nil {
			return err
		}
		existing[id] = struct{}{}
		existing[name] = struct{}{}
		debugTrace("auth materialized id=%s file=%s", id, name)
	}
	return nil
}

// handleModels implements model.static / model.for_auth: the last
// good catalog snapshot mapped to wire ModelInfos; empty catalog yields an
// empty slice, not an error.
func (m *Manager) handleModels() ([]byte, error) {
	m.mu.RLock()
	mgr := m.mgr
	m.mu.RUnlock()
	models := make([]pluginapi.ModelInfo, 0)
	if mgr != nil {
		for _, rec := range mgr.Models() {
			models = append(models, pluginapi.ModelInfo{
				ID:                        rec.PublicID,
				Object:                    "model",
				OwnedBy:                   ProviderID,
				DisplayName:               rec.DisplayName,
				Description:               rec.Description,
				ContextLength:             rec.ContextLimit,
				MaxCompletionTokens:       rec.OutputLimit,
				SupportedInputModalities:  rec.InputModes,
				SupportedOutputModalities: rec.OutputModes,
				Thinking:                  rec.Thinking,
				UserDefined:               rec.UserDefined,
			})
		}
	}
	return okEnvelope(pluginapi.ModelResponse{Provider: ProviderID, Models: models}), nil
}

// Work admission and cancellation share one lock: after quiescing is set,
// no new producer can race a zero-count Wait. Async stream pumps transfer
// ownership before the admitted executor handler releases its count.
func (m *Manager) initWorkContextLocked() {
	if m.workCtx == nil {
		m.workCtx, m.cancelWork = context.WithCancel(context.Background())
	}
}
func (m *Manager) workContext() context.Context {
	m.workMu.Lock()
	defer m.workMu.Unlock()
	m.initWorkContextLocked()
	return m.workCtx
}

func (m *Manager) handleShutdown() ([]byte, error) { return m.handleQuiesce(true) }

func (m *Manager) handleQuiesce(shutdown bool) ([]byte, error) {
	m.drainMu.Lock()
	defer m.drainMu.Unlock()
	m.workMu.Lock()
	m.initWorkContextLocked()
	m.quiescing = true
	m.cancelWork()
	m.workMu.Unlock()
	// Do not hold lifeMu while waiting: an admitted reconfigure may need it
	// to exit. Its HTTP operations already see the canceled work context.
	m.work.Wait()
	m.lifeMu.Lock()
	defer m.lifeMu.Unlock()
	if done := m.closeStop(); done != nil {
		<-done
	}
	if m.bridge != nil {
		// The host closes HTTP resources before shutdown. Never return over a
		// live FFI frame, even when a broken non-HTTP callback refuses to exit:
		// Unix frees host_api and dlclose's immediately after this export.
		m.bridge.inFlight.Wait()
	}
	if shutdown {
		m.mu.Lock()
		m.cfg, m.mgr = config.Config{}, nil
		m.mu.Unlock()
	}
	return okEnvelope(struct{}{}), nil
}

// startRefreshLoop spawns the single background ticker goroutine over the
// caller-supplied stop/done pair (already stored under m.mu) and the served
// catalog manager. The loop captures its own cfg/mgr/bridge snapshot so
// it never contends on m.mu; reconfigure swaps state and restarts the loop.
func (m *Manager) startRefreshLoop(cfg config.Config, mgr *catalog.Manager, interval time.Duration, stop, done chan struct{}) {
	// F4: tick refresh contexts derive from stopCtx so close(stop) aborts
	// an in-flight Refresh immediately instead of leaving lifeMu held until
	// the old config's request-timeout expires. The watcher goroutine is
	// required because the loop body blocks inside refreshOnce while a tick
	// runs and cannot select on stop itself.
	stopCtx, cancel := context.WithCancel(m.workContext())
	go func() {
		<-stop
		cancel()
	}()
	go func() {
		ticker := time.NewTicker(interval)
		defer close(done)
		defer ticker.Stop()
		defer cancel()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				func() {
					defer func() {
						// CGO bridge calls can panic; a tick must never
						// take the host process down.
						if r := recover(); r != nil && m.bridge != nil {
							_ = m.bridge.Log("error", "catalog refresh panicked", nil)
						}
					}()
					refreshOnce(stopCtx, mgr, m.bridge, cfg.RequestTimeout, cfg)
				}()
			}
		}
	}()
}

// closeStop signals a running loop to exit and clears the stop/done pair.
// It returns the loop's done channel — nil when no loop was running — and
// the CALLER waits on it after this returns, never while holding m.mu (F4).
func (m *Manager) closeStop() chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stop == nil {
		return nil
	}
	done := m.done
	close(m.stop)
	m.stop, m.done = nil, nil
	return done
}

// refreshOnce reads the public catalog without sending any configured key.
// Catalog availability is independent of credential cooldown and billing.
func refreshOnce(parent context.Context, mgr *catalog.Manager, bridge *HostBridge, timeout time.Duration, cfg config.Config) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	err := mgr.Refresh(ctx, "")
	if err != nil {
		if bridge != nil {
			_ = bridge.Log("warn", "catalog refresh failed", map[string]any{"error": err.Error()})
		}
		return err
	}
	// surface normalization diagnostics with the post-refresh log.
	warnings := mgr.Warnings()
	if uns := mgr.Unsupported(); len(uns) > 0 {
		parts := make([]string, len(uns))
		for i, u := range uns {
			parts[i] = fmt.Sprintf("%q: %s", u.UpstreamID, u.Reason)
		}
		fields := map[string]any{"models": strings.Join(parts, ", ")}
		if len(warnings) > 0 {
			fields["warnings"] = strings.Join(warnings, "; ")
		}
		_ = bridge.Log("warn", "unsupported models excluded from routable catalog", fields)
	} else if len(warnings) > 0 {
		_ = bridge.Log("warn", "catalog normalization warnings",
			map[string]any{"warnings": strings.Join(warnings, "; ")})
	}
	return nil
}

func okEnvelope(result any) []byte {
	raw, _ := json.Marshal(result)
	out, _ := json.Marshal(pluginabi.Envelope{OK: true, Result: raw})
	return out
}

func ErrEnvelope(code, message string) []byte {
	out, _ := json.Marshal(pluginabi.Envelope{OK: false, Error: &pluginabi.Error{Code: code, Message: message}})
	return out
}

// Use a readable filename because management clients may show it instead of Label.
func availableAuthName(label string, existing map[string]struct{}) string {
	base := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, label)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "OpenCode"
	}
	runes := []rune(base)
	if len(runes) > 80 {
		base = string(runes[:80])
	}
	// Device basenames remain reserved when followed by .json on Windows.
	// Escape on all platforms so credential names stay portable.
	if isWindowsDeviceName(base) {
		base = "OpenCode-" + base
	}
	name := base + ".json"
	for n := 2; ; n++ {
		collision := false
		for existingName := range existing {
			if strings.EqualFold(name, existingName) {
				collision = true
				break
			}
		}
		if !collision {
			return name
		}
		name = fmt.Sprintf("%s-%d.json", base, n)
	}
}

// base has already been sanitized: no dots, spaces, or superscript digits.
// https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file
func isWindowsDeviceName(base string) bool {
	upper := strings.ToUpper(base)
	switch upper {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(upper) == 4 && (upper[:3] == "COM" || upper[:3] == "LPT") && upper[3] >= '1' && upper[3] <= '9'
}
