# 0036. The map's columns by name: an older or a newer map still reads

- Status: accepted
- Date: 2026-11-04

## Context

The snapshot was a list of columns in a fixed order, and `index_version` one number: any column
added made every map of the previous release unreadable (`rebuild the map`), and a map of a newer
build unreadable by an older binary. A job using a map another job built (a CI artifact) failed
whenever the two ran different releases, also when the answer needed nothing new.

## Options

- **Keep the positional format**: every new column is a new format and a rebuild.
- **Columns by name** (as Protocol Buffers fields, Parquet columns): a reader skips a name it does
  not know and reads a missing one empty; the version moves only when a column changes its meaning.
- **Rebuild on the fly**: a query cannot, where the classes are not compiled (a CI artifact).

## Decision

Snapshot format 4: every column has a name (`out.line`, `openedby.shared.node`), the reader indexes
them first. `index_version` is `MAJOR.MINOR` (14.0): a query refuses another major (a column changed
what it holds) and reads another minor. A column the map lacks reads empty: the answer leaves out
what it would hold, names it in `meta.index.missing_columns` and warns on stderr.

## Consequences

- 0.14 and older maps (format 3) still need one rebuild; from 0.15 a new column needs none.
- A column changes its meaning only under a new name, or with a new major.
- `build` still rebuilds when the binary changed (`tool_sha1`): the build hook keeps a fresh map;
  only a map read by another release is read as it is.
