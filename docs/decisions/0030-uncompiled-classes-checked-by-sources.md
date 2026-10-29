# 0030. Classes not compiled: the map is checked by its sources

- Status: accepted
- Date: 2026-10-29

## Context

A query compares the map's inputs with the files: class files, sources, resources, manifests, each
by size, mtime and SHA-1 (0009). A CI job that takes the map as an artifact of another job runs on a
fresh checkout of the same commit, with no class file yet: every class of the map was removed, the
query called the map out of date with tens of thousands of class files "changed", and `--strict`
failed (exit 3) though the map is that commit's. The only way out was `--no-check`, which hides a
map that really is out of date too.

## Options

- **A message for missing classes**: names the state right, still exit 3; the advice is
  `--no-check`.
- **By the commit**: the map is fresh when HEAD is its `commit` and the tree is clean. Needs git
  (optional, 0010), misses an edit not committed, and a map built from a dirty tree never passes.
- **`--strict` split by kind**: a flag to tolerate missing classes; every consumer has to learn it,
  and a missing class among present ones is a real change.
- **By the sources**: a class directory with no class file is not compiled, not changed; the map is
  checked by the sources, resources and manifests it records already.

## Decision

By the sources. A class directory of the map with no class file at all leaves its classes out of
the stale check and names its module in `meta.index.uncompiled` (schema 4.4.0); the sources,
resources and manifests are checked as before, so an edited one still makes the map stale and names
the module to compile. A directory that lost some of its classes is a change as before. `build`
still counts such classes: it cannot rebuild the map without them.

## Consequences

- A map from a fresh checkout of its commit answers with exit 0 under `--strict`, no warning.
- The check trusts that the sources give the classes the map has. A change it does not see (a
  dependency version, a build script) it did not see before either: those are no input of the map.
- The answer is still the bytecode's (0001); only the check reads the sources, by hash.
