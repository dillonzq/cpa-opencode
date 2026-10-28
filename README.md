# cpa-opencode

<p align="center">
  <img src="assets/logo.svg" alt="OpenCode" width="200" height="200">
</p>

<p align="center">
  <strong>English</strong> · <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="#install">install</a> ·
  <a href="#configuration">configuration</a> ·
  <a href="#protocol-selection">protocols</a> ·
  <a href="#account-names-and-quotas">quotas</a> ·
  <a href="https://github.com/dillonzq/cpa-opencode/releases">releases</a> ·
  <a href="CONTRIBUTING.md">contributing</a>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-555" alt="License: MIT"></a>
  <a href="https://help.router-for.me/plugin/development"><img src="https://img.shields.io/badge/CLIProxyAPI-8.0.0%2B-555" alt="CLIProxyAPI 8.0.0+"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.26.10%2B-555" alt="Go 1.26.10+ for source builds"></a>
  <a href="https://github.com/dillonzq/apix/tree/v0.1.0"><img src="https://img.shields.io/badge/apix-0.1.0-555" alt="apix 0.1.0"></a>
</p>

---

A native dynamic Go plugin for [CLIProxyAPI](https://help.router-for.me/plugin/development) (CPA) that exposes [OpenCode pay-as-you-go](https://opencode.ai/v2/docs/console/models/) as a single provider, **`opencode`**.

Configure one credential pool for all models and client protocols. The plugin handles model discovery, upstream routing and protocol conversion; CPA selects credentials and manages rotation, retries, session affinity and cooldowns.

The plugin ID is `cpa-opencode`. The provider ID, credential type and default model prefix are `opencode`.

## Features

- **One model namespace.** Discovered models appear as `opencode/<model-id>` in CPA's `/v1/models`.
- **Four client protocols.** Chat Completions, Messages, Responses and Gemini work with ordinary and streaming requests. The client's own protocol is preferred when the model serves it.
- **Tools and reasoning.** [apix v0.1.0](https://github.com/dillonzq/apix/tree/v0.1.0) converts supported media, function tools, tool results and readable reasoning; native payloads retain their API-specific fields.
- **Live model catalog.** Public discovery, optional models.dev metadata, explicit model declarations and per-model protocol/endpoint overrides.
- **Multiple named credentials.** Stable auth identities and selected-key execution; CPA shares rotation and cooldown state across protocols.
- **Native PAYG quotas.** Per-credential member budgets, balance and available-credit summaries through CPA v8 management endpoints.
- **Targeted cooldown recovery.** After a top-up or transient failure, restore a credential through CPA's native cooldown reset endpoint.
- **Cancellable HTTP.** Client disconnects, timeouts and plugin shutdown cancel the actual upstream operation; incomplete streams report errors.

## Requirements

- **CLIProxyAPI:** `v8.0.0+` with native plugins enabled.
- **Source builds only:** Go `1.26.10+`, CGO and a C compiler targeting the CPA host platform. Prebuilt libraries do not require a Go toolchain.
- **Upstream:** an OpenCode API key for a workspace with pay-as-you-go access. Quota reads also require the relevant Console permissions.

## Install

Choose a build for the operating system and architecture **running CPA**, including its container environment. Builds are available through this repository's [Actions](https://github.com/dillonzq/cpa-opencode/actions/workflows/build.yml) and [Releases](https://github.com/dillonzq/cpa-opencode/releases), or can be [built locally](#build).

| CPA host | Architecture | Library |
| --- | --- | --- |
| Linux | amd64 / arm64 | `cpa-opencode.so` |
| macOS | amd64 / arm64 | `cpa-opencode.dylib` |
| Windows | amd64 | `cpa-opencode.dll` |

1. Verify the archive's SHA-256 against its checksum file.
2. Extract the library and `LICENSE`. Put the library in CPA's plugin directory, for example `plugins/linux/amd64/cpa-opencode.so`.
3. Add the [minimal configuration](#minimal-configuration) and set the upstream API key in CPA's process environment.
4. Restart CPA and check that the plugin loads successfully.
5. Query `/v1/models` with a CPA client key and make a request to an `opencode/<model-id>` model.

## Configuration

Settings belong under `plugins.configs.cpa-opencode`. Both the global plugin switch and the plugin's own switch must be enabled. Unknown plugin fields are rejected; environment variables expand only in `credentials[].api-key`.

### Minimal configuration

```yaml
config-version: 8
plugins:
  enabled: true
  configs:
    cpa-opencode:
      enabled: true
      credentials:
        - name: Personal
          api-key: "${OPENCODE_API_KEY}"
```

Set `OPENCODE_API_KEY` in the environment of the CPA process. Requests to CPA use a **CPA client key**; the OpenCode key above is used upstream. An unset environment variable expands to an empty value and fails validation.

### Multiple credentials

```yaml
credentials:
  - name: Personal
    api-key: "${OPENCODE_API_KEY}"
  - name: Team
    api-key: "${OPENCODE_TEAM_API_KEY}"
    # workspace-id: org_example
```

Each entry accepts `name`, `api-key` and optional `workspace-id`. Modern API keys already select a workspace. An explicit workspace ID becomes `x-org-id` on Console requests and `x-opencode-org-id` on inference requests.

### Configuration options

The [complete configuration example](config.example.yaml) includes the default catalog, metadata, prefix and timeout settings.

| Setting | Default | Meaning |
| --- | --- | --- |
| `base-url` | `https://opencode.ai/inference` | PAYG inference base; protocol paths are appended |
| `catalog-url` | `{base-url}/v1/models` | Public model discovery |
| `console-url` | `https://opencode.ai/console` | Member budget and billing APIs |
| `model-prefix.enabled` | `true` | Prefix public model IDs |
| `model-prefix.value` | `opencode` | Public model prefix |
| `catalog.refresh-interval` | `15m` | Catalog refresh interval, at least `1m` |
| `catalog.stale-while-unavailable` | `true` | Keep the last good catalog during outages |
| `models-dev.enabled` | `true` | Fill missing model metadata from models.dev |
| `models-dev.url` | `https://models.dev/api.json` | Metadata source; uses its `opencode` provider |
| `models-dev.refresh-interval` | `24h` | Metadata refresh interval, at least `1m` |
| `quota.timeout` | `15s` | Quota lookup timeout, at most `1m` |
| `request-timeout` | `5m` | Total upstream request timeout, including streams |
| `max-response-bytes` | `67108864` | Non-streaming inference response limit: 64 MiB |
| `allow-http` | `false` | Enable plain HTTP for local testing |

Endpoint paths append to the configured base without removing version suffixes. Public catalog refresh sends no credential, so a failed or cooled key does not block discovery. User model declarations override discovered metadata, then optional models.dev data supplies missing fields.

## Protocol selection

The default model-family routes follow OpenCode's [endpoint table](https://opencode.ai/v2/docs/console/models/):

| Model family | Chat Completions | Messages | Responses | Gemini |
| --- | :---: | :---: | :---: | :---: |
| `gpt-*`, `grok-*`, `muse-spark-*`, `muse-wonder-*` | — | — | ✓ | — |
| `claude-*`, `qwen*` except `qwen3.8-max*` | — | ✓ | — | — |
| `glm-*`, `kimi-*`, `minimax-*`, `mistral-*`, `longcat-*`, `mimo-*`, `step-*`, `beam-*`, `hy*`, `big-pickle`, `qwen3.8-max*` | ✓ | — | — | — |
| `deepseek-*` | ✓ | ✓ | ✓ | — |
| `gemini-*` | — | — | — | ✓ |
| Other IDs | — | — | — | — |

DeepSeek also serves Messages and Responses. Unknown families need an explicit `models[].protocols` declaration; a model with all protocols disabled is excluded from the routable catalog. Free-tier models may require the OpenCode client even when they appear in discovery.

For each request, the plugin uses the first available protocol in the client's order:

| Client format | Order |
| --- | --- |
| Chat Completions | Chat Completions → Responses → Messages → Gemini |
| Responses | Responses → Chat Completions → Messages → Gemini |
| Messages | Messages → Responses → Chat Completions → Gemini |
| Gemini | Gemini → Responses → Chat Completions → Messages |

| Upstream protocol | Path below `base-url` |
| --- | --- |
| Chat Completions | `/openai/v1/chat/completions` |
| Messages | `/anthropic/v1/messages` |
| Responses | `/openai/v1/responses` |
| Gemini | `/google/v1beta/models/{model}:generateContent` |

Gemini streams use `:streamGenerateContent?alt=sse`. Every route authenticates with `Authorization: Bearer <key>`. For example, a Claude-format request to `opencode/deepseek-v4-pro` uses Messages directly; a Responses request to the same model uses Responses directly.

### Model and protocol overrides

Add these settings inside the plugin configuration:

```yaml
models:
  - name: custom-model
    display-name: Custom
    max-context-length: 128000
    max-tokens: 8192
    input-modalities: [text, image]
    output-modalities: [text]
    thinking:
      levels: [none, low, medium, high]
      zero-allowed: true
    protocols:
      responses:
        enabled: true
        endpoint: /openai/v1/responses
```

`models[].protocols` adds or disables routes and overrides model-specific paths. Top-level `protocols` only overrides paths globally. A listed protocol is enabled unless `enabled: false`. Gemini endpoint overrides must include `{model}`. Model endpoint overrides take precedence over global overrides and defaults.

## Request behavior

- Native requests preserve their API-specific fields. Cross-protocol conversion preserves supported text, images, function tools, tool results and readable reasoning within the target API's capabilities. Unsupported tool types and malformed input return descriptive errors.
- Thinking suffixes such as `opencode/glm-5.3(high)` select reasoning effort and take precedence over body controls. The suffix is removed before upstream routing.
- DeepSeek thinking `none` becomes disabled thinking on Chat Completions and Messages. DeepSeek image `detail: original` becomes `high` where required by the gateway.
- Native GPT Responses preserves its own state. Other Responses models omit foreign encrypted reasoning and retain usable readable reasoning history.
- Every inference request carries `x-opencode-session`, from CPA's canonical session ID, explicit session headers or a stable hash of initial user content.
- Client disconnects, timeouts and quiesce cancel the actual upstream operation. Truncated streams fail, and the plugin does not replay a stream after output starts.

## Account names and quotas

On registration, each configured key receives a separate CPA credential file. `name` controls its display label; stable key-derived IDs survive renaming and reordering. Existing credential files and host metadata are preserved. Removing a configured key stops its execution and per-auth model publication; its physical file can be removed through CPA credential management.

CPA owns selection, rotation and cooldown across protocols. Each execution and quota query uses the selected credential. API keys have no token-renewal flow; `auth.refresh` validates the key and preserves storage, proxy and disabled state.

### Fetch quota

Management operations use a **CPA management key**. Find the target `auth_index` with `GET /v8/management/credentials`, then fetch fresh quota:

```bash
curl -X POST http://localhost:8317/v8/management/credentials/quota/fetch \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"auth_index":"<selected-auth-index>"}'
```

`GET /v8/management/plugins/cpa-opencode/quota?auth_index=<index>` and POST to that plugin route also work.

| Console source | CPA display |
| --- | --- |
| `/api/policies/current-budgets` | Budget groups, remaining fractions and UTC reset times, with per-model/shared scope preserved |
| `/api/billing/status` | Separate USD summaries for balance, available credits and credit limit when present |

Every fetch queries the selected key afresh. Amounts are converted precisely from microcents; balances have no invented percentage denominator. Read access depends on the key's permissions and role; service accounts may not expose member budgets or billing. Partial success returns readable data and logs a redacted source warning. If neither source is readable, the fetch returns an explicit error. An empty budget list reports PAYG plan information. No browser cookie is required.

### Clear cooldown

After a top-up or transient failure, restore only the selected credential's local routing state:

```bash
curl -X POST http://localhost:8317/v8/management/routing/cooldown/reset \
  -H "Authorization: Bearer $CPA_MANAGEMENT_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"auth_index":"<selected-auth-index>"}'
```

Quota reads preserve execution cooldowns. Cooldown reset leaves OpenCode spending and credits unchanged; a new upstream 402 or 429 can cool the key again. Server-side quota reset is unsupported (`supports_reset: false`).

## Build

Use the Go version required by `go.mod`, CGO and a C compiler. Choose the library extension for the target platform:

```bash
# Linux
mkdir -p dist
go build -trimpath -buildmode=c-shared -o dist/cpa-opencode.so .
```

```bash
# macOS
go build -trimpath -buildmode=c-shared -o dist/cpa-opencode.dylib .
```

```powershell
# Windows
go build -trimpath -buildmode=c-shared -o dist/cpa-opencode.dll .
```

Version metadata defaults to `0.0.0-dev`. Inject a version with `-ldflags '-X github.com/dillonzq/cpa-opencode/internal/plugin.pluginVersion=0.1.0'`. Debug diagnostics require a debug build and `CPA_OPENCODE_DEBUG=1`; production builds omit the logger.

## Testing

```bash
go mod download all
go test ./... ./.github/scripts/...
go test -tags debug ./...
go test -tags debug -race ./...
go vet -tags debug ./... ./.github/scripts/...
```

Linux/macOS debug integration tests load the production shared library into CPA v8 through a temporary Go source overlay. Local mock servers verify execution, streams, cancellation, per-credential quota, 402 failover and targeted cooldown reset. The module cache is not modified; `go mod download all` caches the SDK dependencies before the offline host tests.

Real paid inference and account balance access require suitable credentials and separate live verification. Default tests make no paid calls. CI builds Linux amd64/arm64, macOS amd64/arm64 and Windows amd64 packages, each containing the library and license, with an archive checksum.

## License and assets

[MIT](LICENSE). The OpenCode icon is available as [SVG](assets/logo.svg) and [PNG](assets/logo.png). See [asset notes](assets/README.md) for brand resources and the plugin icon URL.
