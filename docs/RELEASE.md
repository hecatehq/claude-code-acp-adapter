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

Use semantic version tags, including alpha prereleases while the adapter is not
production-ready:

```sh
git tag v0.1.0-alpha.1
git push origin v0.1.0-alpha.1
```

Pushing a `v*` tag runs `.github/workflows/release.yml`, which uses GoReleaser
to build Linux, macOS, and Windows binaries for amd64 and arm64. The CLI version
is injected with `-ldflags` into `internal/app.Version`.

The release workflow publishes checksums with the GitHub release. Binary
signing/provenance is not configured yet; add that before calling the release
artifacts production-grade.
