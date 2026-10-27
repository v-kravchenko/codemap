# 0015. A map per build variant, found by `--variant`

- Status: accepted
- Date: 2026-10-27

## Context

A map holds one build variant, set by `variant` in `codemap.yml`. Looking at another one (a release
build with R8, a flavor) meant editing the file, which on a project keeps it untracked and shared by
every run, and rebuilding over the only map. CI wants the debug and the release map side by side,
and an answer that says which of them it came from.

## Decision

- `codemap build --variant X` and a query's `--variant X` override `variant` for one run; the flag
  wins over the file, the file over the preset (`debug`).
- `codemap.yml` names a directory, `snapshot_dir` (default `build/codemap`), not a file: the map of
  a variant is always `<variant>.snap` in it. A query with `--variant X` reads the map
  `build --variant X` wrote, and no setting can make two variants share a file. The former
  `snapshot` key (a file path) is refused with a pointer to `snapshot_dir`.
- `--variant` with `--db` is an error: the map at `--db` has its own variant.
- `meta.index.variant` in `--json` names the variant of the map (schema 3.6.0).
- One variant per `build`: the settings are package globals (ROADMAP, Later), and a run per variant
  costs nothing a shell loop does not do.

## Consequences

- The default map moved from `build/codemap/codemap.snap` to `build/codemap/debug.snap`; the old
  file stays until removed, and the queries ask for `codemap build` once. A `codemap.yml` with
  `snapshot` must say `snapshot_dir` instead.
- `--out`/`--db` still take any file, for a one-off map.
- A release map is read from the classes before R8 (`compileReleaseKotlin`), so minification does
  not change it; obfuscated stack traces are a separate feature (`mapping.txt`, ROADMAP).
- Comparing two maps is not done yet; each answer names its variant, so a script can.

## Alternatives considered

- **`{variant}` in the `snapshot` file path** (`build/codemap/{variant}.snap`). Keeps any file
  name, but a path without the placeholder holds one variant only and needs an error for the
  rest; a directory leaves nothing to get wrong.
- **A suffix for other variants only** (`codemap.snap` for the configured one,
  `codemap-release.snap` for the rest). Keeps the old path, but the name of a map then depends on
  what the file says today, and two rules where one does.
- **`--out`/`--db` only.** No new rule, but `build --variant release` without `--out` writes over
  the debug map, which is the mistake the flag invites.
- **Several variants in one map**, each node and edge tagged. Answers "what is only in release" in
  one query, but changes the snapshot format and needs the settings off globals; ROADMAP, Later.
