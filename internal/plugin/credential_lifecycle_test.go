package plugin

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestRemovedCredentialCannotExecuteOrPublishModels(t *testing.T) {
	f := &fakeCaller{responder: catalogResponder(true, testCatalogJSON)}
	m := NewManager(NewHostBridge(f.call))
	t.Cleanup(func() { _, _ = m.HandleCall(pluginabi.MethodPluginShutdown, nil) })
	before := "credentials:\n  - name: A\n    api-key: key-a\n  - name: B\n    api-key: key-b\nmodels-dev:\n  enabled: false\n"
	decodeEnv(t, mustHandle(t, m, pluginabi.MethodPluginRegister, lifecycleRequestBody(before)))
	req := pluginapi.AuthModelRequest{AuthProvider: ProviderID, Attributes: map[string]string{"api_key": "key-b"}}
	var models pluginapi.ModelResponse
	decodeResult(t, mustHandle(t, m, pluginabi.MethodModelForAuth, mustJSON(req)), &models)
	if len(models.Models) != 1 {
		t.Fatal("configured credential did not publish models")
	}
	after := strings.ReplaceAll(before, "  - name: B\n    api-key: key-b\n", "")
	decodeEnv(t, mustHandle(t, m, pluginabi.MethodPluginReconfigure, lifecycleRequestBody(after)))
	decodeResult(t, mustHandle(t, m, pluginabi.MethodModelForAuth, mustJSON(req)), &models)
	if len(models.Models) != 0 {
		t.Fatal("removed credential still publishes models")
	}
	for _, stream := range []bool{false, true} {
		method := pluginabi.MethodExecutorExecute
		if stream {
			method = pluginabi.MethodExecutorExecuteStream
		}
		out := mustHandle(t, m, method, execReqBodyWithKey("opencode/glm-5.3", "openai", []byte(ccRequestBody), stream, "key-b"))
		env := decodeEnv(t, out)
		if env.OK || env.Error.HTTPStatus != 401 {
			t.Fatalf("removed credential executed: %s", out)
		}
	}
	if _, err := m.FetchQuota(context.Background(), pluginapi.QuotaFetchRequest{Provider: ProviderID, Attributes: map[string]string{"api_key": "key-b"}}); err == nil {
		t.Fatal("removed credential queried quota")
	}
	if len(f.callsOf(pluginabi.MethodHostAuthSave)) != 2 {
		t.Fatal("reconfigure rewrote credential files")
	}
}

func TestAuthParsingAndRefreshPreserveDisabledProxyMetadata(t *testing.T) {
	raw := []byte(`{"type":"opencode","id":"stable","api_key":"key","workspace_id":"org","disabled":true,"proxy_url":"socks5://proxy.test:1080","prefix":"group","label":"Work","custom":{"retained":true}}`)
	p := authProvider{}
	parsed, err := p.ParseAuth(context.Background(), pluginapi.AuthParseRequest{Provider: ProviderID, FileName: "Work.json", RawJSON: raw})
	if err != nil || !parsed.Auth.Disabled || parsed.Auth.ProxyURL != "socks5://proxy.test:1080" || parsed.Auth.Prefix != "group" || parsed.Auth.Metadata["api_key"] != nil {
		t.Fatalf("auth state lost: %+v %v", parsed.Auth, err)
	}
	var expected map[string]any
	_ = json.Unmarshal([]byte(`{"retained":true}`), &expected)
	if !reflect.DeepEqual(parsed.Auth.Metadata["custom"], expected) {
		t.Fatal("custom metadata lost")
	}
	refreshed, err := p.RefreshAuth(context.Background(), pluginapi.AuthRefreshRequest{AuthProvider: ProviderID, AuthID: parsed.Auth.ID, StorageJSON: raw, Attributes: parsed.Auth.Attributes, Metadata: parsed.Auth.Metadata})
	if err != nil || !refreshed.Auth.Disabled || refreshed.Auth.ProxyURL != parsed.Auth.ProxyURL || refreshed.Auth.Prefix != parsed.Auth.Prefix || string(refreshed.Auth.StorageJSON) != string(raw) {
		t.Fatalf("refresh changed state: %+v %v", refreshed.Auth, err)
	}
}
