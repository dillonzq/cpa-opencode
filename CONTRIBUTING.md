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

## Releasing

The first release is `v0.1.0`. Keep the newest release section at the start of
`RELEASE_NOTES.md`; its heading must match the tag exactly. Stable `vX.Y.Z` tags
are supported by the release workflow.

1. Run the checks above, commit the release changes and push them to `main`.
2. Wait for the main branch's Build workflow to pass, then create and push an
   annotated tag for that tested commit:

   ```sh
   git tag -a v0.1.0 -m "Release v0.1.0"
   git push origin v0.1.0
   ```

3. Wait for the tagged Build workflow. It validates the release notes, reruns
   the tests and builds all five platform packages with version `0.1.0` embedded.
   Only after every build succeeds does it create a GitHub Release **draft**
   containing the version's notes, five ZIP archives and their SHA-256 files.
4. Review the draft and verify the package for the intended CPA host before
   publishing it from GitHub Releases. Local mock tests do not verify live paid
   inference or access to a real account's billing data.

Rerunning the tagged workflow updates an existing draft. It refuses to modify
an already published release; fixes to published artifacts require a new version.
