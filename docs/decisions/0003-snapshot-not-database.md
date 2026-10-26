# 0003. A mapped binary snapshot, not a database

- Status: accepted
- Date: 2026-10-24

## Context

The original prototype stored the map in SQLite: nodes, edges, and several derived tables, loaded
into memory for every query. Loading alone cost real time — on a large app (~305k nodes, ~574k
edges) loading from SQLite into the prototype's in-memory graph took about 0.6-1.0s before a query
could even start, and the on-disk database itself ran to 116-281MB depending on which derived
tables it carried.

During the Go rewrite, other storage options for a code graph of this shape were also considered:
column-oriented analytical databases (good at scanning, not at the kind of point lookups and graph
walks these queries do), and embedded graph databases (either unmaintained, or built around a
query language that can't express the project's specific traversal rules — stopping at a screen,
not walking through DI-glue, preferring one shortest path by a stable tie-break — without dropping
back to procedural code anyway).

## Decision

Store the built map as a columnar binary snapshot (struct-of-arrays, with edges as forward and
reverse CSR adjacency lists indexed by node), loaded by memory-mapping the file (`mmap` on
Linux/macOS, a plain read on Windows) rather than deserializing it into Go data structures.

A SQLite exporter (`export --sqlite`) existed during the port, used only to run the same table-by-
table comparison against the prototype's SQLite output as a correctness check. It was removed once
the tool became a standalone project with its own golden tests (0011) and no more prototype to
compare against.

## Consequences

- Loading a snapshot of a large app's map is close to free: the measured load-and-walk time for a
  wide query dropped to about 0.02s once the binary format replaced SQLite, and the snapshot
  (around 75 MB for a large app, strings not deduplicated) is smaller than the equivalent
  database.
- No SQL, no schema migrations, no query planner: every query is a Go function walking slices
  directly, so project-specific traversal rules (stop at a screen, skip DI glue, break ties by a
  stable id order) are ordinary code instead of a workaround bolted onto a query language.
- The format is internal to this tool: nothing else can open the snapshot without linking this
  code, and any format change bumps the index version with no migration path: queries refuse a
  map of another version and ask for `codemap build`.
- Correctness during the rewrite depended on having a second, independent format (SQLite) to diff
  the new implementation against, table by table; without that same safety net now, format changes
  lean on the golden tests (0011) instead.

## Alternatives considered

- **Keep SQLite as the primary store.** Rejected: loading a few hundred thousand rows for every
  query is overhead a binary mmap doesn't pay, and the project's traversal rules don't map cleanly
  onto SQL.
- **An OLAP/columnar analytical database (e.g. DuckDB).** Built for scanning and aggregating large
  tables, not for the point-to-point graph walks (shortest path, bounded BFS with custom stop
  rules) these queries need; rejected without a prototype.
- **An embedded graph database.** One actively-developed candidate existed at evaluation time but
  had since been archived; server-based graph databases (Neo4j/Memgraph-class) require running a
  separate service, which conflicts with being a single CLI binary, and still don't express this
  project's stop/skip/tie-break rules in their query language without procedural code around them.
- **A reference JVM bytecode-to-SQLite tool's design (method-impl table built at index time,
  precomputed bidirectional inheritance).** Its indexing ideas were reused conceptually (resolving
  overrides once, at build time, rather than per query), but it has no incremental update story of
  its own — full rebuild only — and doesn't need the Spring/servlet/decompilation features it also
  carries, so it wasn't adopted wholesale.
- **Datalog-based analysis (Soufflé/Doop-style) or a resident daemon process.** Rejected without a
  prototype: a daemon conflicts with the tool being a stateless CLI invoked per command, and
  Datalog's fixed-point evaluation model doesn't fit a tool meant to run cold, once, in CI.
