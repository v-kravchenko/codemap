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
