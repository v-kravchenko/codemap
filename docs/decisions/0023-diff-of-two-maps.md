# 0023. Two maps compared by node id: `codemap diff --base FILE`

- Status: accepted
- Date: 2026-10-27

## Context

A map holds one variant (0015) and one revision. What a release build lacks (a debug-only screen,
a path only a flavor has) or what a branch adds to the navigation was read by running the same
queries on two maps and comparing the answers by hand. `changes --base-db` (0022) already loads a
second map and matches nodes by id.

## Options

- **`codemap variants diff debug release`** (ROADMAP): names variants, finds their maps in
  `snapshot_dir`; cannot compare two revisions of one variant.
- **Two positional files** (`codemap diff A.snap B.snap`): explicit, but unlike every other query,
  which reads its map by `--db`/`--variant` and checks it is fresh.
- **`--base FILE` against the map of the query**: the map is found and checked as for any query,
  `--base` is any other map, a variant's (`build/codemap/release.snap`) or a revision's (`build
  --out`), as `changes --base-db` takes it.

## Decision

`codemap diff --base FILE`. It compares screens (by node id), the manifest's entries into them
(screen id, kind, detail) and opens (opener's and opened screen's ids, as `graph` draws them),
`added` what only the map has, `removed` what only the base has. Symbols are left out: thousands of
rows a reviewer does not read, and `changes` names the symbols of a change. Maps built with other
settings than the variant warn (exit 3 with `--strict`): their screens differ with no change of
code. The text lists the opens between screens both maps have and counts those of added and
removed screens, which come with them; `--json` lists all.

## Consequences

- A debug-only screen, a release-only path, a branch's new routes and the screens they open show in
  one command, in 0.1 s: opens are precomputed in each map.
- A screen moved to another class is removed and added, as in `changes --base-db`.
- An open is a pair of screens: one gained another way (another hub, another host) is not a
  difference.
