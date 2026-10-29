# 0031. A failed query names its kind in `--json`, exit 2 kept

- Status: accepted
- Date: 2026-10-29

## Context

Every user error exited 2 with a line on stderr, `--json` or not (0009): bad arguments, no map, a
symbol not found, an ambiguous one, a symbol no screen runs. A CI script could not tell a typo in its
call from a symbol the map does not have, nor why it does not have it: a module not compiled, a test
source, or nothing of that name. The audit asked to tell "not found", "unsupported" and "incomplete"
apart; an empty answer that is incomplete is told already (`missing_modules`, `stale_inputs`,
`changes`' `not_covered`, `outside` entries), a failure was not.

## Options

- **An exit code per kind**: a shell tells them without JSON; 0009's codes change, and a code per
  reason does not scale.
- **A completeness field in every answer**: repeats `meta.index`; a failure still says nothing.
- **An `error` object in `--json`**: the kind, and for not found the reason; exit 2 as before.

## Decision

The `error` object. With `--json` a failed query prints `{schema_version, command, error: {kind,
reason, module, message}}` on stdout (schema 4.5.0); stderr and exit 2 stay. A file argument that
names nothing is told by where it is: under a missing module's directory (the model's module names
are their directories), not compiled; on disk and no input, not an input; else not in the map. A
symbol name is not looked for in the sources: it is not in the map, and the message says when the
map misses modules it may be in.

## Consequences

- A script reads `error.kind` and `error.reason`; the message is for people and may change.
- No `meta` in an error: the map may not have loaded.
- A symbol of a module not compiled is `not_in_map`, not `not_compiled`: telling it would mean
  reading the sources (0001).
