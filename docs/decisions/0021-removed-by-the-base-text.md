# 0021. Removed screens: declarations the base text has and the head has not

- Status: accepted
- Date: 2026-10-27

## Context

A change that deletes an activity, a fragment or a preview took it out of the map before `changes`
read it: a deleted file came out as `not_covered: deleted` with no word of the screens in it, and a
class removed from a kept file not at all. A reviewer wants to see what went, and to check that
nothing opens it any more.

## Options

- **The base text** (as for added symbols, docs/decisions/0019): the base's text of each deleted or
  changed source, its top-level classes whose supertypes reach the configured screen bases (through
  the imports, the package and the map's hierarchy) and its functions with a preview annotation;
  removed when neither the map nor the head's text of the file has them. Needs the base's text: git
  mode and `--base` have it, a `--diff` alone only for a deleted file (its removed lines are the
  whole file).
- **A map of the base** (`changes --base-db`): exact, from the bytecode of both revisions, but the
  base would have to be compiled too.

## Decision

The base text. `changes` gives `removed: [{kind, name, target, module, location}]` (the location in
the base), `null` when nothing tells it (`--files` without `--base`), as `changed[].status`.

## Consequences

- A screen moved to another class name or another file's facade is removed in its old place and new
  in its new one, as a symbol moved is added (0019).
- Routes are not covered: a route is a node the map derives from navigation calls, not a declaration
  of the source. Nested classes, previews inside classes and screens whose base the map no longer
  has are not either.
- A map of the base stays possible later for what text cannot tell.
