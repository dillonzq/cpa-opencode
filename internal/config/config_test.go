package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func requireErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

// withKey satisfies the at-least-one-key rule (spec 04 §3) for fixtures
// that target a later validation check.
const withKey = "credentials:\n  - api-key: sk-dummy\n"

func TestModelMetadataOverrides(t *testing.T) {
	c, err := Load([]byte(withKey + `models:
  - name: glm-test
    display-name: Custom
    max-context-length: 200000
    max-tokens: 0
    input-modalities: []
    output-modalities: [text]
    thinking:
      min: 0
      max: 1000
      zero-allowed: false
      dynamic-allowed: true
      levels: []
`))
	if err != nil {
		t.Fatal(err)
	}
	m := c.Models[0]
	if m.Name != "glm-test" || m.DisplayName == nil || *m.DisplayName != "Custom" || m.Context == nil || *m.Context != 200000 ||
		m.Output == nil || *m.Output != 0 || m.InputModalities == nil || len(m.InputModalities) != 0 ||
		m.Thinking == nil || m.Thinking.ZeroAllowed == nil || *m.Thinking.ZeroAllowed || m.Thinking.Levels == nil {
		t.Fatalf("CPA override presence lost: %+v", m)
	}
	if !c.ModelsDev.Enabled || c.ModelsDev.URL != "https://models.dev/api.json" || c.ModelsDev.RefreshInterval != 24*time.Hour {
		t.Fatalf("fallback defaults: %+v", c.ModelsDev)
	}
}

func TestModelMetadataValidation(t *testing.T) {
	for _, yaml := range []string{
		"models: [{name: ''}]", "models: [{name: glm}, {name: glm}]",
		"models: [{name: glm, max-context-length: -1}]", "models: [{name: glm, thinking: {min: 10, max: 1}}]",
		"models-dev: {url: http://metadata.test/api.json}", "models-dev: {refresh-interval: 1s}",
	} {
		if _, err := Load([]byte(withKey + yaml)); err == nil {
			t.Errorf("accepted invalid metadata config %s", yaml)
		}
	}
}

func TestLoadMinimalAppliesAllDefaults(t *testing.T) {
	c, err := Load([]byte(withKey))
	if err != nil {
		t.Fatalf("Load(minimal) failed: %v", err)
	}
	if c.BaseURL != "https://opencode.ai/inference" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.CatalogURL != "https://opencode.ai/inference/v1/models" {
		t.Errorf("CatalogURL = %q", c.CatalogURL)
	}
	if !c.ModelPrefix.Enabled || c.ModelPrefix.Value != "opencode" {
		t.Errorf("ModelPrefix = %+v", c.ModelPrefix)
	}
	if len(c.Credentials) != 1 || c.Credentials[0].Value != "sk-dummy" {
		t.Errorf("Credentials = %+v", c.Credentials)
	}
	if c.Catalog.RefreshInterval != 15*time.Minute {
		t.Errorf("RefreshInterval = %v", c.Catalog.RefreshInterval)
	}
	if !c.Catalog.StaleWhileUnavailable {
		t.Errorf("StaleWhileUnavailable = false")
	}
	if c.Protocols != nil {
		t.Errorf("Protocols = %+v", c.Protocols)
	}
	if c.AllowHTTP {
		t.Errorf("AllowHTTP = true")
	}
	if c.RequestTimeout != 5*time.Minute {
		t.Errorf("RequestTimeout = %v", c.RequestTimeout)
	}
	if c.MaxResponseBytes != 67108864 {
		t.Errorf("MaxResponseBytes = %d", c.MaxResponseBytes)
	}
}

func TestLoadFullConfigHonorsEveryField(t *testing.T) {
	t.Setenv("TEST_OG_KEY", "test-secret-value")
	doc := `
enabled: true
console-url: https://console.test
quota:
  timeout: 12s
base-url: http://localhost:9090/api
catalog-url: https://example.com/models
model-prefix:
  enabled: true
  value: myprefix
credentials:
  - api-key: ${TEST_OG_KEY}
catalog:
  refresh-interval: 1h
  stale-while-unavailable: false
protocols:
  responses:
    endpoint: /v1/global-responses
  gemini: {}
models:
  - name: gpt-5.6-luna
    protocols:
      responses:
        endpoint: /openai/v1/responses
      messages:
        enabled: true
      chat-completions:
        enabled: false
      gemini:
        enabled: false
allow-http: true
request-timeout: 5m
max-response-bytes: 1024
`
	c, err := Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load(full) failed: %v", err)
	}
	if c.BaseURL != "http://localhost:9090/api" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
	if c.CatalogURL != "https://example.com/models" {
		t.Errorf("CatalogURL = %q", c.CatalogURL)
	}
	if !c.ModelPrefix.Enabled || c.ModelPrefix.Value != "myprefix" {
		t.Errorf("ModelPrefix = %+v", c.ModelPrefix)
	}
	if len(c.Credentials) != 1 || c.Credentials[0].Value != "test-secret-value" {
		t.Errorf("Credentials = %+v", c.Credentials)
	}
	if c.Catalog.RefreshInterval != time.Hour {
		t.Errorf("RefreshInterval = %v", c.Catalog.RefreshInterval)
	}
	if c.Catalog.StaleWhileUnavailable {
		t.Errorf("StaleWhileUnavailable = true")
	}
	wantGlobal := map[string]ProtocolOverride{"responses": {Endpoint: "/v1/global-responses"}, "gemini": {}}
	if !reflect.DeepEqual(c.Protocols, wantGlobal) {
		t.Errorf("Protocols = %+v", c.Protocols)
	}
	on, off := true, false
	want := map[string]ProtocolOverride{
		"responses":        {Endpoint: "/openai/v1/responses"},
		"messages":         {Enabled: &on},
		"chat-completions": {Enabled: &off},
		"gemini":           {Enabled: &off},
	}
	if len(c.Models) != 1 || !reflect.DeepEqual(c.Models[0].Protocols, want) {
		t.Errorf("Models = %+v", c.Models)
	}
	if !c.AllowHTTP {
		t.Errorf("AllowHTTP = false")
	}
	if c.RequestTimeout != 5*time.Minute {
		t.Errorf("RequestTimeout = %v", c.RequestTimeout)
	}
	if c.MaxResponseBytes != 1024 {
		t.Errorf("MaxResponseBytes = %d", c.MaxResponseBytes)
	}
}

func TestProtocolEntryForms(t *testing.T) {
	doc := withKey + "models:\n  - name: m\n    protocols:\n      responses:\n      messages: {}\n      gemini: {enabled: true}\n      chat-completions: {enabled: false}\n"
	c, err := Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	got := c.Models[0].Protocols
	for name, want := range map[string]bool{"responses": true, "messages": true, "gemini": true, "chat-completions": false} {
		p, ok := got[name]
		if !ok || p.IsEnabled() != want {
			t.Errorf("%s: entry = %+v (present %v), want enabled %v", name, p, ok, want)
		}
	}
}

func TestLoadEnvExpansion(t *testing.T) {
	t.Setenv("TEST_OG_KEY", "expanded-secret")
	c, err := Load([]byte("credentials:\n  - api-key: ${TEST_OG_KEY}\n"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(c.Credentials) != 1 || c.Credentials[0].Value != "expanded-secret" {
		t.Fatalf("Credentials = %+v", c.Credentials)
	}
}

func TestLoadRejectsUnknownSettings(t *testing.T) {
	for _, extra := range []string{"routing: {}", "excluded-models: []", "route-overrides: {}", "future-key: 1", "api-keys: []", "quota: {timout: 5s}"} {
		if _, err := Load([]byte(withKey + extra)); err == nil {
			t.Errorf("accepted unknown setting %s", extra)
		}
	}
}

func TestPAYGCredentialAndQuotaConfiguration(t *testing.T) {
	t.Setenv("PAYG_TEST_KEY", "  selected-key  ")
	cfg, err := Load([]byte("credentials:\n  - name: Team\n    api-key: ${PAYG_TEST_KEY}\n    workspace-id: org_team\nconsole-url: https://console.test/workspace/\nquota:\n  timeout: 12s\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Credentials[0].Value != "selected-key" || cfg.Credentials[0].WorkspaceID != "org_team" || cfg.ConsoleURL != "https://console.test/workspace" || cfg.QuotaTimeout != 12*time.Second {
		t.Fatalf("incorrect PAYG config: %+v", cfg)
	}
	for _, doc := range []string{
		withKey + "quota: {timeout: 0s}", withKey + "quota: {timeout: 61s}", withKey + "quota: {timeout: invalid}",
		withKey + "console-url: http://console.test", withKey + "---\ncredentials: []",
		"credentials: [{api-key: 'same'}, {api-key: 'same'}]", "credentials: [{api-key: key, workspace-id: \"bad\\nheader\"}]",
	} {
		if _, err := Load([]byte(doc)); err == nil {
			t.Fatalf("invalid config accepted: %s", doc)
		}
	}
}

func TestLoadDerivedCatalogURLFromCustomBase(t *testing.T) {
	c, err := Load([]byte("base-url: https://upstream.example.com/api\n" + withKey))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if c.CatalogURL != "https://upstream.example.com/api/v1/models" {
		t.Errorf("CatalogURL = %q", c.CatalogURL)
	}
}

func TestLoadBaseURLPreservesConfiguredPath(t *testing.T) {
	for _, base := range []string{"https://opencode.ai/inference", "https://opencode.ai/inference/", "https://upstream.example/v1", "https://upstream.example/v1/"} {
		c, err := Load([]byte("base-url: " + base + "\n" + withKey))
		if err != nil {
			t.Fatal(err)
		}
		want := strings.TrimRight(base, "/")
		if c.BaseURL != want || c.CatalogURL != want+"/v1/models" {
			t.Fatalf("base path rewritten: %+v", c)
		}
	}
}

// An EXPLICIT catalog-url ending in "/" must normalize the same way as the
// derived default, so the stored endpoint never carries a trailing slash.
func TestLoadExplicitCatalogURLTrimsTrailingSlash(t *testing.T) {
	c, err := Load([]byte("catalog-url: https://mirror.example.com/api/models///\n" + withKey))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if c.CatalogURL != "https://mirror.example.com/api/models" {
		t.Errorf("CatalogURL = %q, want trailing slashes trimmed", c.CatalogURL)
	}
}

func TestPrefixDisabledSkipsValueValidation(t *testing.T) {
	doc := `
model-prefix:
  enabled: false
  value: "has space"
` + withKey
	c, err := Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := PublicID(c, "glm-5.2"); got != "glm-5.2" {
		t.Errorf("PublicID = %q", got)
	}
}

func TestPublicID(t *testing.T) {
	enabled := Config{ModelPrefix: ModelPrefix{Enabled: true, Value: "opencode"}}
	if got := PublicID(enabled, "glm-5.2"); got != "opencode/glm-5.2" {
		t.Errorf("PublicID(enabled) = %q", got)
	}
	disabled := Config{ModelPrefix: ModelPrefix{Enabled: false}}
	if got := PublicID(disabled, "glm-5.2"); got != "glm-5.2" {
		t.Errorf("PublicID(disabled) = %q", got)
	}
}

func TestLoadRejections(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"invalid yaml syntax", "[unclosed", "decode config"},
		{"unknown anchor has no line info", "request-timeout: *nope\n", "decode config: invalid YAML structure"},
		{"bare-scalar api key never leaks", "credentials:\n  - sk-live-secret-123\n", "decode config: invalid YAML structure"},
		{"keyless config rejected", "", "credentials: at least one key is required"},
		{"request-timeout zero rejected", "request-timeout: 0s\n" + withKey, "request-timeout: must be positive"},
		{
			"duplicate protocol keys",
			"protocols:\n  gemini: {}\n  gemini: {}\n",
			"decode config: invalid YAML structure near line",
		},
		{
			"protocol entry not a mapping",
			"models:\n  - name: bad-model\n    protocols:\n      responses: /v1/x\n" + withKey,
			"decode config: invalid YAML structure near line",
		},
		{
			"model protocol bool shorthand",
			"models:\n  - name: bad-model\n    protocols:\n      responses: false\n" + withKey,
			"decode config: invalid YAML structure near line",
		},
		{
			"global protocol unsupported",
			"protocols:\n  grpc: {}\n" + withKey,
			`protocols: unsupported protocol "grpc"`,
		},
		{
			"global protocol bool shorthand",
			"protocols:\n  gemini: false\n" + withKey,
			"decode config: invalid YAML structure near line",
		},
		{
			"global protocol enabled field",
			"protocols:\n  responses:\n    enabled: true\n" + withKey,
			"protocols.responses: only endpoint is allowed; switch protocols under models[].protocols",
		},
		{
			"global endpoint without leading slash",
			"protocols:\n  responses:\n    endpoint: v1/x\n" + withKey,
			"protocols.responses.endpoint: must start with /",
		},
		{
			"global gemini endpoint without model placeholder",
			"protocols:\n  gemini:\n    endpoint: /google/v1beta/models\n" + withKey,
			"protocols.gemini.endpoint: gemini endpoint must contain {model}",
		},
		{
			"key expands to empty",
			"credentials:\n  - api-key: ${TEST_OG_DEFINITELY_UNSET_VAR}\n",
			"credentials[0].api-key: expanded to empty",
		},
		{
			"second key expands to empty",
			"credentials:\n  - api-key: literal-ok\n  - api-key: ${TEST_OG_DEFINITELY_UNSET_VAR}\n",
			"credentials[1].api-key: expanded to empty",
		},
		{"explicitly empty base-url", `base-url: ""`, "base-url: must not be empty"},
		{"explicitly empty catalog-url", `catalog-url: ""`, "catalog-url: must not be empty"},
		{"malformed base-url", `base-url: "::::"`, "base-url: invalid URL"},
		{"scheme-less base-url", "base-url: opencode.ai/inference", "base-url: invalid URL"},
		{"malformed catalog-url", `catalog-url: "http://exa mple.com"`, "catalog-url: invalid URL"},
		{"http base-url without allow-http", "base-url: http://localhost:9090/v1", "base-url: http scheme requires allow-http"},
		{"http catalog-url without allow-http", "catalog-url: http://localhost:9090/models", "catalog-url: http scheme requires allow-http"},
		{"ftp base-url", "base-url: ftp://files.example.com/v1", `base-url: unsupported scheme "ftp"`},
		{"file catalog-url", "catalog-url: file://host/share/models", `catalog-url: unsupported scheme "file"`},
		{"ftp base-url with allow-http", "allow-http: true\nbase-url: ftp://files.example.com/v1", `base-url: unsupported scheme "ftp"`},
		{"file catalog-url with allow-http", "allow-http: true\ncatalog-url: file://host/share/models", `catalog-url: unsupported scheme "file"`},
		{"refresh-interval missing unit", "catalog:\n  refresh-interval: 5\n", "catalog.refresh-interval: invalid duration"},
		{"refresh-interval negative", "catalog:\n  refresh-interval: -5m\n" + withKey, "catalog.refresh-interval: must be at least 1m"},
		{"refresh-interval below floor", "catalog:\n  refresh-interval: 1ms\n" + withKey, "catalog.refresh-interval: must be at least 1m"},
		{"request-timeout unparsable", "request-timeout: bogus\n", "request-timeout: invalid duration"},
		{"request-timeout explicitly empty", `request-timeout: ""`, "request-timeout: invalid duration"},
		{"request-timeout zero rejected", "request-timeout: 0s\n" + withKey, "request-timeout: must be positive"},
		{"userinfo credentials never echoed", "base-url: https://user:sekret@/v1", "base-url: invalid URL https://"},
		{"max-response-bytes zero rejected", "max-response-bytes: 0\n" + withKey, "max-response-bytes: must be positive"},
		{"max-response-bytes negative rejected", "max-response-bytes: -5\n" + withKey, "max-response-bytes: must be positive"},
		{
			"model protocols in list form",
			"models:\n  - name: bad-model\n    protocols:\n      - protocol: responses\n" + withKey,
			"decode config: invalid YAML structure near line",
		},
		{
			"model protocol unsupported",
			"models:\n  - name: bad-model\n    protocols:\n      grpc: {}\n" + withKey,
			`models[0].protocols: unsupported protocol "grpc"`,
		},
		{
			"model endpoint without leading slash",
			"models:\n  - name: bad-model\n    protocols:\n      responses:\n        endpoint: v1/x\n" + withKey,
			"models[0].protocols.responses.endpoint: must start with /",
		},
		{
			"gemini endpoint without model placeholder",
			"models:\n  - name: bad-model\n    protocols:\n      gemini:\n        endpoint: /google/v1beta/models\n" + withKey,
			"models[0].protocols.gemini.endpoint: gemini endpoint must contain {model}",
		},
		{"prefix enabled empty value", "model-prefix:\n  enabled: true\n  value: \"\"\n" + withKey, "model-prefix.value: invalid provider-ID characters"},
		{"prefix with space", "model-prefix:\n  value: has space\n" + withKey, "model-prefix.value: invalid provider-ID characters"},
		{"prefix leading dash", "model-prefix:\n  value: -lead\n" + withKey, "model-prefix.value: invalid provider-ID characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(tt.yaml))
			requireErrContains(t, err, tt.wantErr)
			if tt.name == "key expands to empty" && strings.Contains(err.Error(), "${") {
				t.Errorf("error leaks environment reference: %q", err.Error())
			}
		})
	}
}

// TestLoadErrorsNeverLeakDecodedValues pins the security property: decode
// and validation failures must not echo raw scalars (a bare-scalar API key)
// or embedded userinfo credentials into the error the host logs.
func TestLoadErrorsNeverLeakDecodedValues(t *testing.T) {
	cases := []struct {
		name   string
		yaml   string
		secret string
	}{
		{"bare-scalar api key", "credentials:\n  - sk-live-supersecret-42\n", "sk-live-supersecret-42"},
		{"duplicate api key values", "credentials:\n  - api-key: sk-live-dupsecret-7\n  - api-key: sk-live-dupsecret-7\n", "sk-live-dupsecret-7"},
		{"userinfo credentials", "base-url: https://admin:p4ssw0rd@/v1\n", "p4ssw0rd"},
	}
	for _, tc := range cases {
		_, err := Load([]byte(tc.yaml))
		if err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
		if strings.Contains(err.Error(), tc.secret) {
			t.Fatalf("%s: error leaks secret %q: %v", tc.name, tc.secret, err)
		}
	}
}

// TestValidateURLRejectsQueryFragmentUserinfo pins the fix for bases that
// parse fine but break JoinUpstreamURL: a query or fragment truncates the
// joined path (…/v1?tenant=x/chat/completions), and userinfo leaks
// credentials into every upstream request URL.
func TestValidateURLRejectsQueryFragmentUserinfo(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		httpOK  bool
		wantErr string
	}{
		{"query rejected naming setting", "https://host/v1?tenant=x", false, "base-url: must not contain query"},
		{"fragment rejected", "https://host/v1#frag", false, "must not contain query, fragment, or userinfo"},
		{"userinfo rejected without echoing credentials", "https://user:key@host/v1", false, "must not contain query, fragment, or userinfo"},
		{"clean https passes", "https://host/v1", false, ""},
		{"http with allow-http passes", "http://localhost:9090/v1", true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL("base-url", tt.raw, tt.httpOK)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateURL(%q) = %v, want nil", tt.raw, err)
				}
				return
			}
			requireErrContains(t, err, tt.wantErr)
			if strings.Contains(err.Error(), "key") && strings.Contains(tt.raw, ":key@") {
				t.Errorf("error echoes credentials: %q", err.Error())
			}
		})
	}
}
