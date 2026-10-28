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
