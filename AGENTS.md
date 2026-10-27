# Agent instructions

codemap is a Go CLI that builds the code map of any Android project from its compiled classes and
resources and answers queries over it (README.md). It must not know any particular project: what
differs between projects (base classes, navigation calls, variant, class directories) lives in the
project's `codemap.yml` and in the built-in presets (`internal/config/presets/*.yml`), never in the code.

## Layout

`cmd/codemap` is the CLI; `internal/` holds the packages, each importing only those above it:
`util` (helpers), `classfile` (the class file parser), `config` (codemap.yml, presets, settings),
`project` (modules, `codemap init` and its Gradle model), `android` (resources, manifests,
navigation XML), `snapshot` (the map's format), `build` (classes and resources into a map),
`query` (the walks and the commands over a map). `internal/fixture` gives tests a copy of
`testdata/navapp`.

## Checks

Run after every change:

```
gofmt -l . && go vet ./... && go test ./...
```

- Before a commit that touches more than a line, run `tools/check.sh`: staticcheck, govulncheck,
  `go test -race` and the Windows and macOS builds, with the tools pinned in `tools/go.mod`.
- `testdata/navapp` is an Android app compiled into `classes.zip`; the tests read the zip, not the
  sources. After changing its sources, run `testdata/navapp/update.sh` (Android SDK in
  `local.properties` or `ANDROID_HOME`) and commit the new `classes.zip` with them.
- `testdata/golden` holds the expected `--json` of the CLI on the fixture. When a change means to
  change an answer, rewrite them with `go test -run TestGolden -update` and read the diff before
  committing; an unexpected diff is a regression.
- Check a change on a real project too: `codemap build` there, then the queries it touches.

## Rules

- Gradle runs only in `codemap init`; `build` and the queries read files and never start Gradle
  or reach the network. git is optional: `build` records HEAD when there is one, only `changes`
  needs it.
- Output is deterministic: the same inputs give byte-identical `--json`. Sort, never rely on map
  order. A change to the `--json` shape is a deliberate, documented change.
- A setting a project may need goes into the config schema (`internal/config`: `config.go`, `codemap.schema.json`;
  `TestSchemaCoversConfig` checks they agree) with a default in a preset.
- A change to the `--json` shape updates `docs/json.md` and `schema_version`; a new setting updates
  `docs/config.md`; a decision with real alternatives gets an ADR in `docs/decisions`; a gap found
  and not fixed goes into `docs/limitations.md` or `ROADMAP.md`.
- Comments say why, in the terse style of the surrounding code.
- Commit messages: one sentence on what changed and why it matters; no prefixes.
- A user-visible change adds a line under `[Unreleased]` in `CHANGELOG.md`. A release: rename that
  section to the version, push the tag `vX.Y.Z`; `.github/workflows/release.yml` does the rest
  (docs/decisions/0016).
