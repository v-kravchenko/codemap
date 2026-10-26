# 0006. Nothing project-specific in code

- Status: accepted
- Date: 2026-10-24

## Context

codemap was first written inside one Android app and had its screen base classes, navigation call
signatures, and resource/build layout hard-coded: specific fully-qualified base class names for a
custom navigation library, specific method names that register or open a route, a specific regex
for screen-layout resource file names, a fixed assumption about build variant and source set
names. None of that generalizes to a different Android project, and some of it did not even
generalize cleanly to a second project tried during the rewrite (a single-module app on a
different navigation stack, DI framework, and Gradle plugin combination) — class-directory
discovery, the assumed build variant, and source-set discovery all had to be fixed before it could
run there at all.

Looking at how comparable tools handle this (lint-style static analyzers, dependency-update
tools): the common shape is a YAML or JSON config file, a JSON Schema for it, built-in presets a
project can extend, and a hard error on an unrecognized key rather than silently ignoring a typo.

## Decision

Nothing project-specific lives in code. Everything that differs between Android projects — the
build variant, class directories, source sets, screen base classes, navigation library and its
call signatures, nav-path step costs — lives in `codemap.yml` at the project root plus the tool's
own built-in presets (`internal/config/presets/*.yml`, embedded in the binary).

- `codemap.yml` has an `extends` list that merges in presets before the file's own settings. Lists
  add up from the presets through to the file; three keys instead replace outright
  (`build.class_dirs`, `build.source_sets`, `changes.base`), because adding configurations here
  rather than merging them avoids silently picking up a preset's base branch or class directory
  alongside the project's own. A later scalar setting wins outright.
- Unknown keys in `codemap.yml` are a hard error, not a silent no-op.
- A `codemap.schema.json` is generated from the same config struct a `TestSchemaCoversConfig`-style
  test checks for agreement, so an editor can validate the file as it's written.
- Navigation support for a given library (Navigation Compose type-safe, Navigation Compose string
  routes, Navigation 3, XML navigation graphs with Fragments, a given DI framework's glue patterns)
  is enabled only by extending the matching preset — `nav2-compose`, `nav2-compose-strings`,
  `nav3`, `android`, `dagger-android`, and so on. Without a `codemap.yml` at all, the tool defaults
  to `extends: [android, nav2-compose]`.

## Consequences

- The same binary works, unmodified, across Android projects with different navigation libraries,
  DI frameworks, and build layouts; what changes is the project's own `codemap.yml`, never the
  tool.
- Every new navigation-pattern capability (a different way of registering or opening a route) is
  implemented once, generally, behind a preset flag — not as a special case for one project's call
  signatures. This was verified by running the tool, config-only, against multiple real and sample
  Android codebases that use different navigation libraries, not just the one it was extracted
  from.
- A config mistake (an unknown key, a typo in a class name) fails loudly at `codemap config check`
  instead of silently producing an empty or wrong map.
- The merge rules for list keys were themselves a source of a real bug during development: a list
  key that should have replaced (a default `changes.base` branch name from a preset) instead
  concatenated with the project's own branch name, which is why `build.class_dirs`,
  `build.source_sets`, and `changes.base` are special-cased to replace rather than merge.

## Alternatives considered

- **Keep project-specific values hard-coded, parameterize later if another project needs it.**
  Rejected: this is exactly the state the tool started in, and it already failed to run, unmodified,
  against a second real project during the rewrite — guessing wrong about class directories, the
  build variant, and source sets.
- **One flat list of options with no preset/`extends` mechanism.** Would still require every
  project to spell out every navigation call signature and screen base class from scratch; presets
  exist specifically so a project using a common, unmodified navigation library needs only
  `extends: [...]` and nothing else.
- **Silently ignore unknown config keys.** Rejected in favor of a hard error: a silently-ignored
  typo in a class name or preset name would produce a map that looks complete but is quietly
  missing an entire navigation pattern, which is worse than a config file that refuses to load.
