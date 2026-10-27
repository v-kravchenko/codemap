# 0017. An install script for CI and machines without Go

- Status: accepted
- Date: 2026-10-27

## Context

codemap is meant to run in CI next to the Android build: it reads the classes that build compiled,
so it runs in the same job, on the project's own Android image. That image has no Go and no
Homebrew. Taking a release by hand there means a dozen lines of curl, tar and checksums per project.

## Decision

`install.sh` at the repository root, POSIX sh, Linux and macOS: it takes the archive for the system
from GitHub Releases (`CODEMAP_VERSION`, default the latest, found by the redirect of
`releases/latest` rather than the API and its rate limit), checks it against `checksums.txt` and puts
`codemap` into `CODEMAP_INSTALL_DIR` (default `~/.local/bin`, no root). The CI runs it on Linux
(dash) and macOS against the latest release.

## Consequences

- `curl -fsSL .../main/install.sh | sh` runs the script as it is on main; a CI that wants it fixed
  pins a commit in the URL and the release by `CODEMAP_VERSION`.
- It needs curl and sha256sum or shasum; Windows keeps the zip and `go install`.
- The checksum guards the download, not the source: `gh attestation verify` does that.

## Alternatives considered

- **Homebrew tap.** For developers on macOS, not for a CI image; later, when someone asks (0016).
- **mise / aqua.** Work without anything from us for those who already use them; not something to
  require from a CI image.
- **A Docker image.** codemap needs the classes of the build in the same job, so an image of its own
  only helps as a source to copy the binary from.
- **A Gradle plugin.** Natural for Android projects, but a Kotlin codebase and a plugin portal
  release to keep beside the Go one.
