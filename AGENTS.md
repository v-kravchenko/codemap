# Agent instructions

codemap is a Go CLI that builds the code map of any Android project from its compiled classes and
resources and answers queries over it (README.md). It must not know any particular project: what
differs between projects (base classes, navigation calls, variant, class directories) lives in the
project's `codemap.yml` and in the built-in presets (`presets/*.yml`), never in the code.

## Checks

Run after every change:

```
gofmt -l . && go vet ./... && go test ./...
```

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
- A setting a project may need goes into the config schema (`config.go`, `codemap.schema.json`;
  `TestSchemaCoversConfig` checks they agree) with a default in a preset.
- Comments say why, in the terse style of the surrounding code.
- Commit messages: one sentence on what changed and why it matters; no prefixes.
