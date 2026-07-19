# Release

Releases are tag-driven and built with GoReleaser.

## Before Tagging

Run the local gate:

```sh
make release-check
```

Optionally build a local snapshot archive:

```sh
make snapshot
```

## Tagging

Use semantic version tags. Add a prerelease suffix only for prerelease builds:

```sh
git tag v0.3.0
git push origin v0.3.0
```

Pushing a `v*` tag runs `.github/workflows/release.yml`, which uses GoReleaser
to build Linux, macOS, and Windows binaries for amd64 and arm64. The CLI version
is injected with `-ldflags` into `internal/app.Version`. Before packaging, the
workflow runs the release gate: normal and race test suites, `go vet`, a version
smoke test, and `go mod tidy -diff`. GoReleaser repeats the tidy check and Go
test suite, so packaging cannot silently change module metadata.

The workflow accepts only a SemVer `v*` tag whose commit is reachable from the
default branch. Before creating a stable tag, verify that GitHub rules require
pull-request review and the repository test check on the default branch, and
restrict creation, update, and deletion of `v*` tags to release maintainers.

The release workflow publishes `checksums.txt` with the GitHub release and then
generates GitHub artifact attestations from that checksum file using
`actions/attest`. The attestation binds every release archive named in
`checksums.txt` to the tag workflow that built it.

## Verifying a Release Asset

Download the archive and checksum file for the tag:

```sh
tag=v0.3.0
version="${tag#v}"
archive="claude-code-acp-adapter_${version}_linux_amd64.tar.gz"

gh release download "$tag" \
  --repo hecatehq/claude-code-acp-adapter \
  --pattern "$archive" \
  --pattern checksums.txt
```

Verify the checksum and provenance:

```sh
grep "  ${archive}$" checksums.txt | shasum -a 256 -c -
gh attestation verify "$archive" --repo hecatehq/claude-code-acp-adapter
```

Do not call a release stable until the current
[stable-readiness gate](STABLE_READINESS.md) is green.
