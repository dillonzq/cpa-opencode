package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"

	"github.com/dillonzq/cpa-opencode/internal/config"
)

type authProvider struct{ cfg config.Config }

var _ pluginapi.AuthProvider = authProvider{}

func (authProvider) Identifier() string { return ProviderID }

func (p authProvider) ParseAuth(_ context.Context, req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
	debugTrace("auth parse request provider=%s file=%s raw_bytes=%d", req.Provider, req.FileName, len(req.RawJSON))
	var raw struct {
		Type        string `json:"type"`
		Provider    string `json:"provider"`
		ID          string `json:"id"`
		Label       string `json:"label"`
		APIKey      string `json:"api_key"`
		WorkspaceID string `json:"workspace_id"`
		Disabled    bool   `json:"disabled"`
		ProxyURL    string `json:"proxy_url"`
		Prefix      string `json:"prefix"`
	}
	if err := json.Unmarshal(req.RawJSON, &raw); err != nil {
		if req.Provider == ProviderID {
			return pluginapi.AuthParseResponse{}, fmt.Errorf("opencode auth record has invalid JSON")
		}
		return pluginapi.AuthParseResponse{}, nil
	}
	if (req.Provider != "" && req.Provider != ProviderID) || (raw.Type != ProviderID && raw.Provider != ProviderID) {
		return pluginapi.AuthParseResponse{Handled: false}, nil
	}
	if strings.TrimSpace(raw.APIKey) == "" {
		return pluginapi.AuthParseResponse{}, fmt.Errorf("opencode auth record has no api key")
	}
	if raw.ID == "" {
		raw.ID = credentialID(strings.TrimSpace(raw.APIKey))
	}
	var metadata map[string]any
	_ = json.Unmarshal(req.RawJSON, &metadata)
	delete(metadata, "api_key")
	debugTrace("auth parse handled provider=%s file=%s id=%s api_key_present=%t api_key_length=%d", req.Provider, req.FileName, raw.ID, strings.TrimSpace(raw.APIKey) != "", len(raw.APIKey))
	return pluginapi.AuthParseResponse{Handled: true, Auth: pluginapi.AuthData{
		Provider: ProviderID, ID: raw.ID, FileName: req.FileName, Label: accountLabel(p.cfg, raw.APIKey, raw.Label, 0), StorageJSON: req.RawJSON,
		Disabled: raw.Disabled, ProxyURL: raw.ProxyURL, Prefix: raw.Prefix, Metadata: metadata,
		Attributes: map[string]string{"api_key": strings.TrimSpace(raw.APIKey), "workspace_id": p.cfg.WorkspaceID(strings.TrimSpace(raw.APIKey), raw.WorkspaceID)},
	}}, nil
}

func (authProvider) StartLogin(context.Context, pluginapi.AuthLoginStartRequest) (pluginapi.AuthLoginStartResponse, error) {
	return pluginapi.AuthLoginStartResponse{}, fmt.Errorf("opencode login is unsupported; configure a manual api key")
}

func (authProvider) PollLogin(context.Context, pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
	return pluginapi.AuthLoginPollResponse{}, fmt.Errorf("opencode login is unsupported; configure a manual api key")
}

func (p authProvider) RefreshAuth(_ context.Context, req pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
	debugTrace("auth refresh request provider=%s id=%s storage_json_bytes=%d attr_names=%v metadata_names=%v", req.AuthProvider, req.AuthID, len(req.StorageJSON), mapKeys(req.Attributes), mapKeys(req.Metadata))
	key := strings.TrimSpace(req.Attributes["api_key"])
	if req.AuthProvider != ProviderID || key == "" || !p.cfg.HasCredential(key) {
		return pluginapi.AuthRefreshResponse{}, fmt.Errorf("selected OpenCode credential is unavailable")
	}
	attrs := maps.Clone(req.Attributes)
	attrs["workspace_id"] = p.cfg.WorkspaceID(key, attrs["workspace_id"])
	var stored struct {
		Disabled bool   `json:"disabled"`
		ProxyURL string `json:"proxy_url"`
		Prefix   string `json:"prefix"`
		Label    string `json:"label"`
	}
	if len(req.StorageJSON) > 0 && json.Unmarshal(req.StorageJSON, &stored) != nil {
		return pluginapi.AuthRefreshResponse{}, fmt.Errorf("OpenCode auth storage is invalid")
	}
	disabled := stored.Disabled
	if current, ok := req.Metadata["disabled"].(bool); ok {
		disabled = current
	}
	return pluginapi.AuthRefreshResponse{Auth: pluginapi.AuthData{Provider: ProviderID, ID: req.AuthID, Label: accountLabel(p.cfg, key, stored.Label, 0), Disabled: disabled, ProxyURL: stored.ProxyURL, Prefix: stored.Prefix, StorageJSON: req.StorageJSON, Metadata: req.Metadata, Attributes: attrs}}, nil
}

func credentialID(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "opencode-key-" + hex.EncodeToString(digest[:])
}

func mapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

// Labels are presentation only. Stable auth IDs and stored host metadata are preserved.
func accountLabel(cfg config.Config, key, existing string, fallbackIndex int) string {
	index := fallbackIndex
	for i, entry := range cfg.Credentials {
		if entry.Value == key {
			if name := strings.TrimSpace(entry.Name); name != "" {
				return name
			}
			index = i
			break
		}
	}
	label := strings.TrimSpace(existing)
	if label != "" {
		return label
	}
	if len(cfg.Credentials) > 1 {
		return fmt.Sprintf("OpenCode %d", index+1)
	}
	return "OpenCode"
}
