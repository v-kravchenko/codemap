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
- The map's path depends on the variant: `snapshot` takes `{variant}`, and its default is
  `build/codemap/{variant}.snap`. A query with `--variant X` reads the map `build --variant X` wrote.
- A `snapshot` without `{variant}` holds the configured variant only: `--variant` with another one
  is an error, not a map written over it.
- `--variant` with `--db` is an error: the map at `--db` has its own variant.
- `meta.index.variant` in `--json` names the variant of the map (schema 3.6.0).
- One variant per `build`: the settings are package globals (ROADMAP, Later), and a run per variant
  costs nothing a shell loop does not do.

## Consequences

- The default map moved from `build/codemap/codemap.snap` to `build/codemap/debug.snap`; the old
  file stays until removed, and the queries ask for `codemap build` once.
- A release map is read from the classes before R8 (`compileReleaseKotlin`), so minification does
  not change it; obfuscated stack traces are a separate feature (`mapping.txt`, ROADMAP).
- Comparing two maps is not done yet; each answer names its variant, so a script can.

## Alternatives considered

- **A suffix for other variants only** (`codemap.snap` for the configured one,
  `codemap-release.snap` for the rest). Keeps the old path, but the name of a map then depends on
  what the file says today, and two rules where one does.
- **`--out`/`--db` only.** No new rule, but `build --variant release` without `--out` writes over
  the debug map, which is the mistake the flag invites.
- **Several variants in one map**, each node and edge tagged. Answers "what is only in release" in
  one query, but changes the snapshot format and needs the settings off globals; ROADMAP, Later.
