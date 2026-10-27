# Changelog

What changed in each release, newest first ([Keep a Changelog](https://keepachangelog.com/en/1.1.0/)).
Versions follow [SemVer](https://semver.org/); while they are 0.x a minor release may change the CLI.
The `--json` shape has a version of its own, `schema_version` (docs/json.md). A release takes its
notes from its section here (`tools/release-notes.sh`).

## [Unreleased]

### Added

- `install.sh`: installs a release on Linux or macOS with one line, for CI and machines without Go;
  checks the download against the release's checksums.

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

[Unreleased]: https://github.com/v-kravchenko/codemap/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/v-kravchenko/codemap/releases/tag/v0.3.0
