# Changelog

What changed in each release, newest first ([Keep a Changelog](https://keepachangelog.com/en/1.1.0/)).
Versions follow [SemVer](https://semver.org/); while they are 0.x a minor release may change the CLI.
The `--json` shape has a version of its own, `schema_version` (docs/json.md). A release takes its
notes from its section here (`tools/release-notes.sh`).

## [Unreleased]

### Added

- A map built while modules were not compiled says so: `meta.index.missing_modules` in every
  `--json`, a warning of every query, exit 3 with `--strict`, `code map: 31 of 78 modules compiled`
  from `build`; their classes compiled since make the map stale. Such a map used to answer as if
  whole. `schema_version` 3.7.0.
- `changes` tells what the change adds: `changed[].status` `added` or `modified`, `screens[].status`
  `new`, `changed` (depth 0) or `affected`, and `(added)`/`new` in the text. A screenshot job no
  longer needs the base's sources to know which previews have no before.
- Screens name their previews: `previews` in `screens`, `impact` and `changes`, a preview's
  `content` (the composables it draws), and `--no-preview` for the screens no preview shows. A
  preview goes with the screen whose own composables it draws, not a theme or a shared component.
  The map format changes (index 10): run `codemap build`.

## [0.4.2] - 2026-10-27

### Fixed

- `codemap wrapper --version X` of another release wrote its own build hook under X's header; it
  now runs release X through codemapw to write X's files, and refuses releases before 0.4.0.

## [0.4.1] - 2026-10-27

### Fixed

- The build hook builds the map when the compile tasks were up to date too: a map that was missing
  or deleted stayed missing until something was recompiled. A fresh map costs the check, ~0.15 s.
- The build hook writes the model again after the build, from the configuration cache: on a cache
  hit the model of an earlier configuration (another branch) stayed, and `build` stopped with "run
  `codemap init`".

## [0.4.0] - 2026-10-27

### Added

- `install.sh`: installs a release on Linux or macOS with one line, for CI and machines without Go;
  checks the download against the release's checksums.
- `codemap wrapper`: writes `codemapw` and `codemapw.cmd`, which run a pinned release checked by
  sha256, and `gradle/codemap.gradle`, which a project applies in its settings so that each build
  writes the model and rebuilds the map of each variant it compiled. After a pull, a build is all
  a developer needs before `./codemapw` answers.

### Changed

- `codemap build` takes over the model the build hook wrote (`build/codemap/gradle-model.json`)
  when it is newer than the build files, so `codemap init` is not needed with the hook.
- Two `codemap build` at once no longer share a temp file: each writes the map through its own.

### Security

- Built with Go 1.27.3: `codemap wrapper` downloads over TLS, and 1.27.2 trusted compromised
  intermediate certificates (GO-2026-6809).

## [0.3.0] - 2026-10-27

The first public release.

### Added

- `codemap init`: one Gradle run (configuration only) writes the project model, `codemap.model.json`;
  `build` and the queries never start Gradle.
- `codemap build`: the code map from the compiled classes, resources, manifests and navigation XML
  into a mapped binary snapshot; skipped when nothing changed.
- Queries, each with a stable `--json` (schema 3.6.0): `used-by`, `uses`, `impact`, `changes`,
  `nav-path`, `graph` (Mermaid), `screens`, `find`, `build --info`.
- `--variant` on `build` and the queries: a map per build variant, `<variant>.snap` in
  `snapshot_dir`, and `meta.index.variant` in `--json`.
- `--module` globs on `impact`, `changes`, `used-by`, `uses`, `screens`, `find` and `graph`.
- Presets for Activities/Fragments, Navigation Compose (typed and string routes), Navigation 3 and
  navigation events; anything project-specific goes into `codemap.yml`.
- `codemap version`.

[Unreleased]: https://github.com/v-kravchenko/codemap/compare/v0.4.2...HEAD
[0.4.2]: https://github.com/v-kravchenko/codemap/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/v-kravchenko/codemap/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/v-kravchenko/codemap/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/v-kravchenko/codemap/releases/tag/v0.3.0
