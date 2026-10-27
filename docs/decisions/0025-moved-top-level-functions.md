# 0025. A top-level function moved to another file is the same declaration

- Status: accepted
- Date: 2026-10-27

## Context

`changes --base-db` tells added and removed screens by node id (0022). A top-level Kotlin function
moved to another file of its package keeps its Kotlin name (package, name, parameters) but lives
in another file facade class (`GuideScreenKt` to `GuideScreenPreviewsKt`), so its node id changes:
the move read as a preview removed and one added, the change's own preview with no base to compare
with. On a real change 4 of 12 added previews were such moves.

## Options

- **Keep the node id.** Exact to the JVM; a move is a new screen.
- **Match by name across the package**: a static method of a file facade (`*Kt`) the other map
  lacks, by package, name and descriptor. Kotlin's own identity; but `private` top-level functions
  of one name may be in several files of a package (`Preview()` in each screen's file).
- **Match by name, and only one to one**: as above, when exactly one such function is missing from
  each map.
- **Match by content or source text.** Survives a rename, but a guess, and text is what 0022 left.

## Decision

One to one by package, name and descriptor. The pair is neither added nor removed; the head's
screen gets `moved_from: {name, location}` (the base's). Its members (lambdas) go with it, as they
go with any declaration (0022). A changed status of a moved symbol is `modified`. Schema 4.1.0.

## Consequences

- Two functions of one name moved at once, a move that also changes parameters, or a facade
  renamed by `@file:JvmName` (no `Kt` suffix) stay added and removed: never a wrong pair.
- A moved class (another name to Kotlin too) is still removed and added.
- `codemap diff` stays by node id (0023): two variants rarely differ by a move, and its lists
  are of facts of one map or the other.
