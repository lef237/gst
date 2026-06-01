# Release

This project is distributed primarily as a Go command:

```sh
go install github.com/lef237/gst/cmd/gst@latest
```

Publishing a release means pushing a semver Git tag. Optional prebuilt binaries
can also be attached to a GitHub Release for users who do not have Go installed.

## Versioning

Use semver tags with a leading `v`:

- `v0.1.0` for the first public release
- `v0.1.1` for fixes that do not add behavior
- `v0.2.0` for backward-compatible feature releases
- `v1.0.0` once the CLI behavior is considered stable

Go resolves `@latest` from published module versions, so the most recent semver
tag becomes the version installed by:

```sh
go install github.com/lef237/gst/cmd/gst@latest
```

## Preflight

Start from a clean working tree on `main`:

```sh
git status --short --branch
git pull --ff-only
```

Run the test suite and build a local binary:

```sh
go test ./...
go build -o tmp/gst ./cmd/gst
```

Smoke-test the binary from a Git repository:

```sh
tmp/gst --once
```

## Tag Release

Create an annotated tag for the release:

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
```

Push only the tag when the release is ready:

```sh
git push origin v0.1.0
```

After the tag is available on GitHub, users can install the exact version:

```sh
go install github.com/lef237/gst/cmd/gst@v0.1.0
```

They can also install the latest semver tag:

```sh
go install github.com/lef237/gst/cmd/gst@latest
```

## Optional Binary Assets

Build release binaries into a fresh `tmp/gst-release` directory. If an old
directory is present, discard it first:

```sh
trash tmp/gst-release
```

Then build the assets. Pass the tag through `-ldflags` so the binaries report
the right version from `gst --version` (binaries built this way carry no module
version otherwise, unlike `go install`):

```sh
mkdir -p tmp/gst-release

LDFLAGS="-X main.version=v0.1.0"
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o tmp/gst-release/gst-darwin-arm64 ./cmd/gst
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o tmp/gst-release/gst-darwin-amd64 ./cmd/gst
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o tmp/gst-release/gst-linux-amd64 ./cmd/gst
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o tmp/gst-release/gst-windows-amd64.exe ./cmd/gst
```

Create checksums:

```sh
shasum -a 256 tmp/gst-release/* > tmp/gst-release/checksums.txt
```

Create the GitHub Release and attach the binaries:

```sh
gh release create v0.1.0 tmp/gst-release/* \
  --title "v0.1.0" \
  --notes "See README.md for installation and usage."
```

After the release is complete, the generated directory can be discarded:

```sh
trash tmp/gst-release
```

## Release Checklist

- Working tree is clean.
- `go test ./...` passes.
- `go build -o tmp/gst ./cmd/gst` succeeds.
- `tmp/gst --once` runs in a Git repository.
- The release tag follows semver, such as `v0.1.0`.
- The tag has been pushed to GitHub.
- Optional GitHub Release assets and `checksums.txt` are attached.
