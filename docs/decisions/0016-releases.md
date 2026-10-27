# 0016. Releases by tag: goreleaser, notes from CHANGELOG.md, Codecov

- Status: accepted
- Date: 2026-10-27

## Context

codemap goes public on GitHub. Its users are Android developers, most without Go, so a binary per
system is the main way in; `go install` stays for those with Go. The repository had no CI: every
change was checked by hand with `tools/check.sh`.

## Decision

- CI (`.github/workflows/ci.yml`): `tools/check.sh` on Linux on every push to main and pull request,
  `go test` on macOS and Windows; actions pinned by commit SHA, kept fresh by Dependabot.
- A tag `vX.Y.Z` releases (`.github/workflows/release.yml`): goreleaser builds Linux, macOS and
  Windows for amd64 and arm64 with the version in `main.version`, plus `checksums.txt`; the
  archives get a signed build provenance (`gh attestation verify`).
- Release notes come from the tag's section of `CHANGELOG.md` (Keep a Changelog), written by hand;
  a tag without one fails the release.
- Versions are SemVer, 0.x while the CLI may still change; `schema_version` of `--json` stays a
  version of its own (0009).
- Coverage of all packages by all tests (`-coverpkg=./...`, the golden tests drive most code) goes
  to Codecov by OIDC, no token secret.

## Consequences

- A release is: move `[Unreleased]` to the version in CHANGELOG.md, commit, push the tag.
- Coverage counts code the golden tests run as covered even where no test checks its answer.

## Alternatives considered

- **Notes generated from commits** (goreleaser's changelog, release-please). They group by
  Conventional Commits prefixes, which this repository does not use; a hand-written section says
  what a user notices instead of what each commit did.
- **Homebrew tap.** One more repository and token; later, when someone asks.
- **Coverage without a service** (a badge written to a branch by the CI). No account, but no
  coverage change on a pull request; Coveralls does about what Codecov does with less.
