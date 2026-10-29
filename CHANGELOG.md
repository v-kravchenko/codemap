# Changelog

What changed in each release, newest first ([Keep a Changelog](https://keepachangelog.com/en/1.1.0/)).
Versions follow [SemVer](https://semver.org/); while they are 0.x a minor release may change the CLI.
The `--json` shape has a version of its own, `schema_version` (docs/json.md). A release takes its
notes from its section here (`tools/release-notes.sh`).

## [Unreleased]

## [0.10.2] - 2026-10-29

### Fixed

- A query no longer calls the map out of date where its classes are not compiled, as in a CI job
  using a map built by another job from the same commit: a class directory with no class file is
  not compiled rather than changed, and the map is checked by the sources, resources and manifests
  alone; `meta.index.uncompiled` names those modules (schema 4.4.0).

## [0.10.1] - 2026-10-28

### Fixed

- A build no longer waits minutes on a stalled download of codemapw: the download has timeouts,
  the build hook skips a download that failed in the last 15 minutes, and the build waits on codemap
  3 minutes at most.

## [0.10.0] - 2026-10-28

### Added

- `crash [FILE]` reads a crash's stack trace, as Crashlytics exports it, logcat prints it or Java
  writes it, and answers where it threw, the entry into the app's code and the screens and
  components it ran for: those of the own frame reaching the fewest, so a shared helper that threw
  no longer stands for every screen using it. `--mapping` reads an obfuscated trace by the build's
  R8/ProGuard `mapping.txt`, inline chains and line ranges included (schema 4.3.0).

## [0.9.0] - 2026-10-28

### Added

- `impact` and `changes` list the components a change reaches next to the screens: the application,
  services, receivers and providers of the manifests, and the workers by `components.worker_bases`
  (WorkManager's in the `android` preset); `build --info` counts them. A change reaching only a push
  service or `Application.onCreate` no longer looks like it reaches nothing (`components`, schema
  4.2.0). Maps are rebuilt (index version 12).
- `nav-path` starts a way at a push too: a screen the code of a service or receiver taking a push
  opens (`nav_path.push_actions`, Firebase's in the `android` preset) gets a `push` entry instead of
  `outside`, and `--entry push` starts there.
- A query warns when codemap.yml or its presets give other settings than the map was built with,
  and with `--strict` exits 3: such a map may answer anything differently
  (`meta.index.stale_inputs.config`, schema 4.2.0).

## [0.8.0] - 2026-10-28

### Changed

- A preview drawing a body that its screen's composable calls in another file of the module goes
  with that screen (the one whose composable, Activity or fragment is in the preview's file, when
  several draw the body), and a composable returning a value (a theme getter, `remember*`) is no
  longer in `content` or links a preview to a screen. A route's preview no longer goes with the
  Activity or fragment hosting it too, and a route registered in an Activity's own NavHost without
  captures gets its `content`. Maps are rebuilt (index version 11).

## [0.7.0] - 2026-10-27

### Added

- `changes --base-db`: a top-level Kotlin function (a preview) moved to another file of its package
  is no longer a screen added and one removed: `screens[].moved_from` names it and its location in
  the base (schema 4.1.0).

### Changed

- A module no app depends on (a test helper, `testImplementation` only) is no longer missing from
  the map, so `--strict` does not fail on it; the build's hint for modules not compiled names the
  app's compile tasks instead of each module's. `codemap init` (or `codemap wrapper` for the hook)
  writes the module dependencies this needs.

## [0.6.0] - 2026-10-27

### Added

- `changes --base-db FILE` takes the code map of the base: `changed[].status`, `screens[].added`
  and `removed` come from the two maps by node id and member name, routes and nested screens
  included; `meta.refs.base_map` names its commit and variant, and a map of another commit or
  variant warns (exit 3 with `--strict`).
- `codemap diff --base FILE` compares two maps: the screens, manifest entries and opens between
  screens that only one has, so a debug-only screen or a release-only path shows without reading
  both maps' answers; for two revisions too.

### Removed

- `changes` no longer reads the base's text to tell added from modified and to find removed
  screens: those regular expressions missed routes, nested classes and moved declarations, and
  misjudged a dozen statuses on a real project. Without `--base-db`, `changed[].status` and
  `removed` are `null`.
- `screens[].status` of `changes`: `screens[].added` says whether the screen is new, `depth`
  whether it changed itself (0) or through what it uses.
- `changes --format targets`: `--json` with a JSON tool gives the same lines.
- `schema_version` 4.0.0.

## [0.5.0] - 2026-10-27

### Added

- A map built while modules were not compiled says so: `meta.index.missing_modules` in every
  `--json`, a warning of every query, exit 3 with `--strict`, `code map: 31 of 78 modules compiled`
  from `build`; their classes compiled since make the map stale. Such a map used to answer as if
  whole. `schema_version` 3.7.0.
- `changes` tells what the change adds: `changed[].status` `added` or `modified`, `screens[].status`
  `new`, `changed` (depth 0) or `affected`, and `(added)`/`new` in the text, so a
  consumer knows what has no before without the base's sources.
- Screens name their previews: `previews` in `screens`, `impact` and `changes`, a preview's
  `content` (the composables it draws), and `--no-preview` for the screens no preview shows. A
  preview goes with the screen whose own composables it draws, not a theme or a shared component.
  The map format changes (index 10): run `codemap build`.
- `changes` names the resource qualifiers of a change: `changed[].location` is the changed file
  (`values-night/colors.xml:12`, not `values/`), `changed[].qualifiers` its qualifiers and
  `screens[].qualifiers` those a screen is reached by alone, `only night` in the text: a
  night-only change touches only the night configuration of its screens.
- `changes` names the screens a change removes: `removed[]`, the activities, fragments, dialogs and
  previews the base's text declares and the head no longer has, and `Removed:` in the text.
- `changes --format targets`: a tab-separated line per screen (status, depth, target, module,
  qualifiers), new first, for a script that needs no JSON parser.
- `changes --diff` checks the diff against the sources of the map: a diff of another revision, whose
  lines would name other symbols, gives a warning, and exit 3 with `--strict`.

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

[Unreleased]: https://github.com/v-kravchenko/codemap/compare/v0.10.2...HEAD
[0.10.2]: https://github.com/v-kravchenko/codemap/compare/v0.10.1...v0.10.2
[0.10.1]: https://github.com/v-kravchenko/codemap/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/v-kravchenko/codemap/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/v-kravchenko/codemap/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/v-kravchenko/codemap/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/v-kravchenko/codemap/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/v-kravchenko/codemap/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/v-kravchenko/codemap/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/v-kravchenko/codemap/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/v-kravchenko/codemap/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/v-kravchenko/codemap/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/v-kravchenko/codemap/releases/tag/v0.3.0
