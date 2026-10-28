package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"github.com/dillonzq/cpa-opencode/internal/config"
)

func TestQuotaRegistration(t *testing.T) {
	m := NewManager(nil)
	var registration registrationResult
	decodeResult(t, registrationEnvelope(), &registration)
	if !registration.Capabilities.QuotaProvider || registration.Capabilities.ManagementAPI {
		t.Fatal("incorrect quota capabilities")
	}
	var desc pluginapi.QuotaDescribeResponse
	decodeResult(t, mustHandle(t, m, pluginabi.MethodQuotaDescribe, []byte(`{}`)), &desc)
	if len(desc.SupportedProviders) != 1 || desc.SupportedProviders[0] != ProviderID || desc.SupportsReset {
		t.Fatal("incorrect description")
	}
	for _, method := range []string{pluginabi.MethodManagementRegister, pluginabi.MethodManagementHandle, pluginabi.MethodQuotaReset} {
		var env pluginabi.Envelope
		json.Unmarshal(mustHandle(t, m, method, []byte(`{}`)), &env)
		if env.OK {
			t.Fatalf("obsolete/unsupported operation succeeded: %s", method)
		}
	}
}
func TestReadableAuthFilenameAtColdStartDoesNotDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Personal.json")
	raw := []byte(`{"type":"opencode","api_key":"existing","label":"Personal","disabled":true}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	f := &fakeCaller{responder: func(method string, _ []byte) ([]byte, error) {
		if method == pluginabi.MethodHostAuthList {
			return hostOK(hostAuthListResponse{Files: []pluginapi.HostAuthFileEntry{{Name: "Personal.json", Provider: ProviderID, Path: path}}}), nil
		}
		return hostOK(map[string]any{}), nil
	}}
	m := NewManager(NewHostBridge(f.call))
	if err := m.materializeAuthRecords(context.Background(), config.Config{Credentials: []config.Credential{{Value: "existing"}}}); err != nil {
		t.Fatal(err)
	}
	if len(f.callsOf(pluginabi.MethodHostAuthSave)) != 0 {
		t.Fatal("duplicated existing credential")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("modified host metadata")
	}
}
