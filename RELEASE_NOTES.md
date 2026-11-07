# v0.4.2

## Behavior changes

- `request-timeout` no longer limits an open stream. It covers a whole non-streaming request, and for a stream only the opening, until the upstream status arrives; for status 400 or higher it also covers reading the error body.
- New `stream-idle-timeout` (default `5m`) ends a stream that receives no chunk for that long. Each chunk restarts it, and time spent delivering a chunk downstream does not count.
- New `max-stream-duration` (default `1h`) caps one stream's lifetime from the start of opening. Both new settings must be positive durations.
- Streamed requests to Chat Completions upstreams now set `stream_options.include_usage`, so clients converted from Messages, Responses or Google GenerateContent, and CPA accounting, receive token counts. Native Chat Completions streams also end with the standard usage chunk whose `choices` is empty, even when the client did not ask for it.

## Fixes

- Long but active streams, such as large outputs or long reasoning, are no longer cut off after `request-timeout` (default `5m`) while the upstream is still sending data.
- A stream ended by a limit now reports which one it hit: `stream did not open within request-timeout`, `stream exceeded stream-idle-timeout`, or `stream exceeded max-stream-duration`. Client or shutdown cancellation reports `stream canceled`. An upstream error status whose body read is cut off by a limit keeps its status classification, and its message names the limit.
- A stream whose opening limit expires just as the upstream opens now fails as a retryable network error, instead of opening and then closing immediately.

## Dependencies and platforms

- Upgrade apix from `v0.2.0` to `v0.4.0`. A GenerateContent function call without an `id` converted to another protocol now gets `call_google_N` instead of `call_gemini_N`, and the refusal text for a blocked prompt reads `Request blocked by Google.` A single upstream SSE event larger than 64 MiB now fails the stream.
- Add Windows arm64 and FreeBSD amd64 packages, both cross-compiled on Linux. Release packages now include seven platform ZIPs and one combined `checksums.txt`.
- Documentation names the vendor Google where it named Gemini. The `gemini-*` model family, the `gemini` protocol key and CPA's `gemini` format ID are unchanged.

## Compatibility and verification

- Backward compatible with `v0.4.1`. A `request-timeout` raised only to keep long streams alive can return to its default; raise `max-stream-duration` instead if streams may last over an hour. A stream silent for over 5 minutes is now ended by `stream-idle-timeout`, as it previously was by `request-timeout` at the same default. Requires CLIProxyAPI `v8.0.0+` with native plugins enabled.
- Production tests, native host integration tests, race checks and vet pass against local mock upstreams. Live paid inference and account billing access require separate verification.

# v0.4.1

## Features

- Add `models[].alias` to rename a model's published ID. The alias replaces the upstream `name` in the client-facing ID, the model prefix still applies, and requests still send `name` upstream. For example, `name: minimax-m3` with `alias: MiniMax-M3` publishes `opencode/MiniMax-M3`, or `MiniMax-M3` with the prefix disabled, so CPA can pool it with another provider that publishes the same name.
- Aliases must be unique and must not have surrounding whitespace. When an alias collides with another model's ID, the later model in catalog order is excluded and the reason appears in diagnostics. The original published ID no longer routes after aliasing; the bare upstream ID still does.

## Compatibility and verification

- Backward compatible with `v0.4.0`; configurations without `alias` publish the same model IDs. Requires CLIProxyAPI `v8.0.0+` with native plugins enabled.
- Release packages include five platform ZIPs and one combined `checksums.txt`.
- Production tests, native host integration tests, race checks and vet pass against local mock upstreams. Live paid inference and account billing access require separate verification.

# v0.4.0

## Fixes and dependencies

- Display the plugin brand as `OpenCode` in registration metadata. The plugin ID remains `cpa-opencode`, and the provider ID remains `opencode`.
- Upgrade apix from `v0.1.0` to `v0.2.0` for JSON editing, inline media and Responses stream improvements.

## Compatibility and verification

- No configuration changes from `v0.3.0`. Requires CLIProxyAPI `v8.0.0+` with native plugins enabled.
- Release packages include five platform ZIPs and one combined `checksums.txt`.
- Production tests, native host integration tests, race checks and vet pass against local mock upstreams. Live paid inference and account billing access require separate verification.

# v0.3.0

## Breaking configuration change

- Rename `plugins.configs.cpa-opencode.api-keys[].api-key` to `plugins.configs.cpa-opencode.api-keys[].value`. Update your CPA configuration before upgrading; the old `api-key` entry field is rejected, including when supplied alongside `value`.
- Environment variable expansion now uses `api-keys[].value`. Optional `name` and `workspace-id` fields are unchanged.
- Update configuration metadata, validation messages, examples and both language guides; migrate unit and native host integration fixtures to the new field.

## Compatibility and verification

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
