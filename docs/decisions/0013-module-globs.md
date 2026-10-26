# 0013. `--module` globs as `.gitignore` takes directories

- Status: accepted
- Date: 2026-10-26

## Context

`--module GLOB` limits an answer to some modules: one team's part of an MR's impact, one feature's
screens, `find` without other features' noise, a feature's map in `graph`. Module paths are a tree
(`feature/userdoc`, `feature/userdoc/financial`, `feature/cards/api`, `feature/cards/impl`), and the
most common ask is a feature with its `api`/`impl` modules. A plain glob over the whole path
(`*`: any run of characters) needs two globs for that (`feature/cards`, `feature/cards/*`), and
`feature/cards*` takes `feature/cardsx` too. Zones with owners, planned next, will name modules by
globs in `codemap.yml` the way CODEOWNERS names directories, so the rule should serve both.

## Decision

- A glob takes the module and the modules under it; `*` is a run within one path segment, `**` any
  run of segments; `:a:b` (the Gradle path) is `a/b`.
- `!GLOB` takes back what the globs before it took; the last glob matching a module decides. A
  module no glob matches is kept only when the first glob is a `!` one, so `--module '!feature/debug'`
  alone means everything else.
- A glob that matches no module of the map is an error, not an empty answer (a typo in CI would
  otherwise read as "nothing affected").
- In `impact`, `changes`, `used-by`, `uses`, `screens` and `find` it filters the answer, not the walk:
  a change in `core/network` reaches a screen in `feature/cards` through `core/domain`. In `graph` it
  limits the walk, which is what a feature's map is. `hidden_by_module` counts what it left out.

## Consequences

- One matcher (`internal/query/modules.go`) for every `--module`, ready for zones.
- `graph --module` changed meaning for `*` (it crossed `/`) and for a glob without `*` (now also the
  modules under it); the fixture has one module, so its golden call became `--module !app`.
- The parent module without its children takes two globs (`feature/userdoc`,
  `!feature/userdoc/*`); on the work project one module of 79 has children.

## Alternatives considered

- **Exact by default, `/...` for the subtree (Go, Bazel).** The simplest rule, but the common ask
  needs the suffix every time, and Android developers know `.gitignore` and CODEOWNERS, not Go
  package patterns.
- **The subtree by default plus a marker for the module alone (`=feature/userdoc`, `$`).** Solves
  only that one case and is known from nowhere; `!` also gives "everything but a module".
- **`*` across `/`, as before.** Fewer rules, but `feature/*` would be the same as `feature/**` and
  there would be no way to say one level (`feature/*/impl`).
- **A glob without `/` matching at any depth (`.gitignore`).** Module paths are absolute (Gradle
  paths from the root); `app` matching every `*/app` would surprise more than help, `**/app` says it.
