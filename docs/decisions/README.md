# Architecture Decision Records

An ADR here records a decision with real alternatives — one a later contributor would otherwise
have to re-derive from scratch, by reading code, trying the obvious alternative, and hitting the
same wall. Write one when:

- the decision rejected an option that looks reasonable on its face (a database, an incremental
  update, a different language), and the reason is not visible in the code or the commit message
  alone;
- the decision is easy to accidentally reverse in a later change (someone adds a Gradle call to a
  query, someone adds a project name to a source file) and the record is what stops that review
  comment from having to be re-argued;
- the decision shapes the output contract (`--json` shape, exit codes) in a way other tools now
  depend on.

Do not write one for a decision with no real alternative (there was only one sane way to do it),
or for an implementation detail that is clear from reading the code.

## Format

MADR-lite. Each file: `# NNNN. Title`, then `Status` and `Date`, then `Context`, `Decision`,
`Consequences`, `Alternatives considered` (each alternative with why it was rejected).

## Superseding a decision

Write a new ADR with the next number. The old file gets `- Status: superseded by NNNN` in place
of `accepted`, and nothing else in it changes — an accepted ADR's decision, scope, and rationale
are not rewritten after the fact. A verified factual or documentation correction (a wrong number,
a stale file path) may be fixed directly in place; a changed decision gets a new ADR.

## Index

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-bytecode-not-source.md) | Analyse compiled classes, not source | accepted |
| [0002](0002-go.md) | Go, not Rust/Kotlin/the prototype's language | accepted |
| [0003](0003-snapshot-not-database.md) | A mapped binary snapshot, not a database | accepted |
| [0004](0004-full-rebuild.md) | Full rebuild, not incremental updates | accepted |
| [0005](0005-gradle-only-in-init.md) | Gradle only in `codemap init` | accepted |
| [0006](0006-project-specifics-in-config.md) | Nothing project-specific in code | accepted |
| [0007](0007-nav-path-cost-model.md) | `nav-path` as a Dijkstra walk with step costs | accepted |
| [0008](0008-general-rules-over-project-rules.md) | General type-based rules, never project rules | accepted |
| [0009](0009-stable-json-contract.md) | A stable, versioned `--json` contract | accepted |
| [0010](0010-git-optional.md) | Git is optional, used only by `changes` | accepted |
| [0011](0011-golden-tests.md) | Golden tests on a compiled fixture | accepted |
| [0012](0012-nav-path-via-and-ways.md) | `nav-path --via` as chained walks, `--ways` by the fork | accepted |
| [0013](0013-module-globs.md) | `--module` globs as `.gitignore` takes directories | accepted |
| [0014](0014-route-content-by-smap.md) | A route's content: the composables called where it is registered, by SMAP | accepted |
| [0015](0015-map-per-variant.md) | A map per build variant, found by `--variant` | accepted |
| [0016](0016-releases.md) | Releases by tag: goreleaser, notes from CHANGELOG.md, Codecov | accepted |
| [0017](0017-install-script.md) | An install script for CI and machines without Go | accepted |
| [0018](0018-wrapper-and-build-hook.md) | A wrapper and a build hook: the checkout runs codemap, the build keeps the map fresh | accepted |
| [0019](0019-added-by-the-base-text.md) | Added or modified: a declaration the base text lacks | superseded by 0022 |
| [0020](0020-previews-by-content.md) | A preview goes with the screen whose own composables it draws | accepted |
| [0021](0021-removed-by-the-base-text.md) | Removed screens: declarations the base text has and the head has not | superseded by 0022 |
| [0022](0022-added-and-removed-by-a-base-map.md) | Added and removed by a map of the base | accepted |
| [0023](0023-diff-of-two-maps.md) | Two maps compared by node id: `codemap diff --base FILE` | accepted |
| [0024](0024-missing-modules-by-the-app-graph.md) | Missing modules: those the app depends on | accepted |
| [0025](0025-moved-top-level-functions.md) | A top-level function moved to another file is the same declaration | accepted |
| [0026](0026-previews-of-a-shared-body.md) | A preview of a body screens share goes with its file's screen | accepted |
| [0027](0027-manifest-components.md) | Manifest components and workers: reached, listed apart from screens | accepted |
| [0028](0028-push-entry.md) | A screen a push service opens has a push entry | accepted |
| [0029](0029-crash-screens-by-the-narrowest-frame.md) | A crash's screens are those of its own frame reaching the fewest | accepted |
| [0030](0030-uncompiled-classes-checked-by-sources.md) | Classes not compiled: the map is checked by its sources | accepted |
| [0031](0031-errors-in-json.md) | A failed query names its kind in `--json`, exit 2 kept | accepted |
| [0032](0032-via-of-impact.md) | How a change reaches a screen: `via`, the walk unchanged | accepted |
