# 0022. Added and removed by a map of the base

- Status: accepted
- Date: 2026-10-27

## Context

`changes` tells added symbols from modified ones (0019) and names the screens a change removes
(0021) from the base's text of the changed sources, by regular expressions over Kotlin and Java:
a second source of truth next to the bytecode the rest of the map is built from (0001). Text
misses what the map knows: multi-line headers, `@JvmName`, nested classes, previews inside
classes, routes, and screens removed in a file the diff does not name. A CI job that builds the
map of its target branch anyway (for its own queries, or cached per commit) has the exact answer
at hand.

## Options

- **Keep the text only.** Needs no second build, stays approximate.
- **A map of the base by node id** (`changes --base-db FILE`): a node's id is a hash of its kind,
  owner, name and descriptor, the same in every build of the same declaration, so added is "no
  node of this id in the base's map" and removed is "a screen of the base's map that is not one in
  the head's". Exact for everything the map has, but the base must be compiled and built.
- **A map of the base by name** (label or target): survives a change of descriptor, but overloads
  collide and a compiler-made name (`load$1`, `lambda$0`) is renumbered by any lambda added before
  it.
- **Replace the text by the map.** One source, but `changes` without a compiled base would lose
  status and removed, which 0019 and 0021 gave it.

## Decision

`--base-db` by node id, then by name, and the text heuristics removed. `changed[].status` and
`removed` come from the two maps alone, with `--files` too, and are `null` without a base map;
`screens[].status` gives way to `screens[].added` (`true`/`false`, `null` without), its `changed`
and `affected` being `depth` 0 or more. A node whose id the base's map lacks is still modified when
the base's class has a member of its name (a function whose parameters changed, an overload
added); what the compiler numbers (a lambda, an anonymous class, a ComposableSingletons lambda, a
property initializer) goes with the declaration it is in. `removed` covers the whole map, not
only the changed files: the two maps differ only by the change. `meta.refs.base_map` names the
map's commit, variant and dirtiness; a map of another commit than the base, built dirty or of
another variant than the head's is a warning, exit 3 with `--strict`, as a diff of another
revision is. `--format targets` goes too: a third output contract for one script, which `--json`
and a JSON tool give. `schema_version` 4.0.0.

## Consequences

- Routes can be `new` and `removed`, and nested screens are covered.
- A screen class's id is its name: only a renamed one is new and removed; a preview whose
  parameters change is too (it is not looked up by name: a screen is a node, not a declaration).
- On a real project (611 changed files, 440 changed symbols) the map and the text disagreed on 12
  statuses, each a text miss (a class moved to another package, a property of a nested class
  taken for the outer's), and the map alone found 12 new routes and a removed one.
- A CI job that wants added and removed builds the base too (or keeps its map per commit); one
  that does not gets `null`, never a guess.
- The same loading of two maps is what comparing two variants' maps needs (ROADMAP).
