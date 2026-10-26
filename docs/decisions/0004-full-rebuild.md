# 0004. Full rebuild, not incremental updates

- Status: accepted
- Date: 2026-10-24

## Context

With the original, interpreted prototype, a full rebuild of a large app's map (~35k classes, ~305k
nodes, ~574k edges) took around 9-19s depending on which derived tables were included, which was
too slow to run on every local edit. A substantial effort went into making updates incremental
instead of rebuilding from scratch:

- A full design modeled on how other incremental systems do it (a Meta-style fact-ownership model
  where each fact belongs to the file/module that produced it; a symbol-indexing system's move away
  from sequential global ids toward stable string ids, specifically so part of an index can update
  without the rest; an IDE-style incremental computation engine's input/output memoization with
  early cutoff; a static analyzer's base-plus-overlay model for changed files; a Kotlin compiler's
  lookup-tracker approach of recording which names a function searched for).
- Stable 63-bit node ids by `(kind, name, descriptor)` so a changed file's nodes keep the same id
  across builds.
- A staged rollout ("slices"), each widening what updates incrementally while the rest still falls
  back to a full rebuild, each slice's win measured before doing the next: method-body-only parsing
  and delta writes (19.0s to 9.3s on that build); recomputing which screens' "opened by" walk needs
  to rerun only for screens whose earlier walk actually touched a changed edge (plus a "shortest-
  path membership" rule to keep that correct); recomputing screen registration by ownership instead
  of a full reload.
- A differential test (apply the same random change two ways — incrementally, and as a from-
  scratch build — and assert the two outputs are byte-identical) as the correctness gate for every
  slice.

This work got the incremental path down to roughly 3.5-4s end to end, with real remaining
complexity (owner tracking on every row, a dependency-usage table, orphan cleanup, differential
tests per slice) to go further.

Separately, the Go rewrite (0002) measured a from-scratch build, with none of this machinery, at
about 3s for the same app once parsing and I/O moved off the interpreter and off SQLite (0003).

## Decision

Rebuild the whole map from scratch on every `codemap build`. Do not port the incremental-update
machinery to Go; do not maintain per-row ownership, a dependency-usage table, or a differential
test suite for partial updates.

## Consequences

- A build takes about 3s on a large app; that was explicitly agreed to be an acceptable step
  before incremental work was dropped (the original target that justified incremental work was a
  build time under about 2s).
- The whole incremental design — stable ids as a fact-ownership key, per-row owner tracking,
  dependency-usage accounting, slice-by-slice differential testing — is gone. There is no partial-
  update code path to keep correct, no byte-for-byte-equal-to-a-full-build invariant to maintain
  as the schema evolves, and no risk of an incremental bug silently drifting from a full rebuild.
- `codemap build` already skips work when nothing changed (checks recorded inputs and the tool's
  own version; `--force` overrides this), which covers the common "nothing changed, don't redo the
  work" case without any incremental machinery.
- If build time grows again as the format or the walk gets heavier, the earlier staged-slice
  design (stable ids, ownership, the differential test harness) is the known fallback plan, not a
  new investigation.

## Alternatives considered

- **Port the incremental-update slices as they stood in the original prototype.** Rejected: once a
  from-scratch Go build reached about the same wall-clock time as the incremental path had in the
  old language, the complexity of owner-per-row tracking, dependency-usage tables, and a
  differential test per slice bought nothing a full rebuild didn't already give for free.
- **Push further on incremental design (a Salsa-style fully memoized dependency graph).** Would
  likely have gone faster still, but was not pursued once the full-rebuild number made it
  unnecessary; the "build the fastest full rebuild first, add incrementality only if it's still
  too slow" order was followed, and the second half never became necessary.
- **A resident daemon process that holds the map in memory and updates it on file-watch events.**
  Rejected early (0003) for the same reason a database server was rejected: it conflicts with the
  tool being a stateless CLI, and a cold full rebuild in a few seconds removes most of the reason
  to keep a long-lived process around at all.
