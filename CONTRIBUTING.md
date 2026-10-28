# Contributing

Use the Go version in `go.mod`, enable CGO and install a C compiler.

```sh
go mod download all
go test ./... ./.github/scripts/...
go test -tags debug ./...
go test -tags debug -race ./...
go vet -tags debug ./... ./.github/scripts/...
```

Native host tests run on Linux/macOS and load the actual shared library into the pinned CPA v8 SDK. They use local mock upstreams; they do not need real credentials or make paid requests. Test reports and build products should not be committed.

Maintain credential isolation throughout execution, metadata, workspace selection and quota. Only the CPA host owns routing cooldown state. Quota lookups must not clear cooldowns or substitute another key. Preserve existing credential files and host-owned metadata during registration and reconfiguration.

Never log keys, authorization headers, browser sessions, prompts or tool arguments. Errors that cross the host boundary must be bounded and redacted. Keep stream cancellation tied to its own host operation and callback scope.

Update both READMEs and the configuration example for schema changes. Add release entries to `RELEASE_NOTES.md` under a `# vX.Y.Z` heading.
