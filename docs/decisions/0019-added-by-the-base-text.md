# 0019. Added or modified: a declaration the base text lacks

- Status: accepted
- Date: 2026-10-27

## Context

A CI job that renders the screens a change touches before and after it needs to know which are
new: a new preview has nothing to render before. `changes` gave each screen its depth, so a new
preview and an edited one both came out at depth 0 with nothing to tell them apart. The map has
only one revision, the head's.

## Options

- **By the diff's lines**: a symbol is added when every line of it is an added line. Needs nothing
  but the diff, but an existing function whose body is rewritten reads as new, and its
  before-render is lost. A class has no line range of its own.
- **By the base text**: a symbol is added when its file is new, or the file's text in the base
  does not declare its name (`fun Name(`, `class Name`, `val name`) and the head's does. A rewrite
  stays modified; a declaration renamed reads as added, which it is for a render. Needs the base
  revision: git mode always has it, `--diff`/`--files` with `--base`.
- **A map of the base** (`changes --base-db`): exact, by the bytecode of both revisions, but a CI
  job would have to compile the base too.

## Decision

The base text, with the lines as the fallback for a `--diff` without `--base` (functions only).
`changed[].status` is `added` or `modified` (`null` for `--files` without `--base`: nothing tells
it); a member of an added class is added; a resource is added when every file defining it is new or
lacked its key in the base. `screens[].status` is `new` when the screen itself is added, `changed`
at depth 0, `affected` below: where changed ends and affected starts beyond that is the caller's,
from `depth`. A route is never `new`: its node is not a declaration of the source.

## Consequences

- A preview in a new file, or added to an existing one, is `new`: a consumer comparing before and
  after knows it has no before without exporting the base's sources.
- The match is textual, per file: a symbol moved from another file is `added` in its new one.
- A map of the base stays possible later for what text cannot tell.
