# v0.3.0

## Breaking configuration change

- Rename `plugins.configs.cpa-opencode.api-keys[].api-key` to `plugins.configs.cpa-opencode.api-keys[].value`. Update your CPA configuration before upgrading; the old `api-key` entry field is rejected, including when supplied alongside `value`.
- Environment variable expansion now uses `api-keys[].value`. Optional `name` and `workspace-id` fields are unchanged.
- Update configuration metadata, validation messages, examples and both language guides; migrate unit and native host integration fixtures to the new field.

## Compatibility and verification

- Upgrade apix from `v0.1.0` to `v0.2.0` for JSON editing, inline media and Responses stream improvements.
- Requires CLIProxyAPI `v8.0.0+` with native plugins enabled.
- Release packages include five platform ZIPs and one combined `checksums.txt`.
- Automated tests use local mock upstreams; live paid inference and account billing access require separate verification.

# v0.2.0

## Breaking configuration change

- Rename `plugins.configs.cpa-opencode.credentials` to `plugins.configs.cpa-opencode.api-keys`. Before upgrading, rename this field in your CPA configuration; the old `credentials` field is rejected. Entries keep the same `api-key`, optional `name` and `workspace-id` fields.
- Update plugin configuration metadata, validation messages, examples and both language guides to use `api-keys`.

## Release packages

- Align release packages with plugin store requirements: five platform ZIPs and one combined `checksums.txt`. Each ZIP contains the platform library and `LICENSE` at its root.

## Compatibility and verification

- Fix the quiesce test to track concurrent quota HTTP operations independently and bound cancellation waits.
- Requires CLIProxyAPI `v8.0.0+` with native plugins enabled.
- Verify your platform ZIP against `checksums.txt` before installation.
- Production tests, native host integration tests, race checks and vet cover local mock upstreams; live paid inference and account billing access require separate verification.

# v0.1.0

Initial OpenCode pay-as-you-go provider for CLIProxyAPI v8.

- Register provider `opencode` and discover the PAYG model catalog.
- Serve Chat Completions, Responses, Messages and Gemini requests with streaming, tools, reasoning and cancellation.
- Materialize multiple named credentials with stable identities and selected-key execution.
- Expose per-credential member budgets and USD billing summaries through CPA v8 quota APIs.
- Integrate CPA's native cooldown and targeted cooldown reset after payment or rate-limit failures.
- Include offline native-host integration tests and platform-specific build artifacts.

## Compatibility and installation

- Requires CLIProxyAPI `v8.0.0+` with native plugins enabled and an OpenCode PAYG API key.
- Download the ZIP for the CPA host: Linux amd64/arm64, macOS amd64/arm64, or Windows amd64. Archives use `cpa-opencode_<version>_<goos>_<goarch>.zip` with no leading `v`. Verify the ZIP against the release's `checksums.txt` before extracting the library and `LICENSE` from the ZIP root.
- Follow the [English installation guide](https://github.com/dillonzq/cpa-opencode/blob/v0.1.0/README.md#install) or [中文安装说明](https://github.com/dillonzq/cpa-opencode/blob/v0.1.0/README.zh-CN.md#安装) for the minimal configuration.
- Prebuilt libraries do not require Go. Source builds require Go `1.26.10+`, CGO and a C compiler for the target platform.

## Verification scope

Automated tests use local mock upstreams and the pinned CPA v8 native host. They do not make paid requests. Live inference and real member-budget/billing access depend on the configured key's permissions and require separate verification.
