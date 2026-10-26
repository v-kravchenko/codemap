# 0010. Git is optional, used only by `changes`

- Status: accepted
- Date: 2026-10-24

## Context

The tool was asked, early on, to work even for a project that has no git repository at all, and to
have no git hooks and no git dependency baked into its core analysis path. The only place git is
genuinely needed is `changes`: comparing two refs (or a ref against a diff/file list) requires
asking git what changed.

One corner of the implementation violated this without anyone noticing at first: finding which
methods reference a Kotlin `const val` (a compile-time-inlined constant with no bytecode call
edge back to it, see 0001) was implemented by shelling out to `git grep` across the working tree,
filtered to `/src/main/`. This is a real contradiction of the stated rule, found during an audit of
everything that depends on a specific project's layout or tooling: it assumes a git repository
exists, assumes a particular source-set layout, and reaches outside the files the map itself
already knows about.

## Decision

Only `changes` touches git (and only when it needs a ref comparison; `build` records the current
HEAD commit into the map's metadata opportunistically, when a repository happens to be present,
purely as provenance information, not as a dependency). Every other command — including finding the
users of a constant — works from files the map's own build already read and recorded, never by
invoking git or assuming a repository exists.

Concretely, `constUsers` (used by `used-by`/`uses`/`impact` for a constant field) scans the map's
own recorded Kotlin/Java source files for a textual reference to the constant's name, filtered to
files that either declare themselves in the constant's package or import it (by name, by owning
class, or by wildcard) — the same source list the build already walked to discover classes and
resources, not a repository-wide search, and not scoped to any one hard-coded source-set name.

## Consequences

- A project with no git repository at all (or one running the tool against an unpushed worktree)
  can still run every command except `changes`.
- `const val` resolution, which touches source text rather than bytecode, now has the same
  "only reads inputs the build itself recorded" property as the rest of the map, and does not
  assume a particular source-set directory name — fixing both the git dependency and the
  project-specific path assumption in the same change, since they were the same piece of code.
- `changes` is the one command whose behavior genuinely depends on the state of a git working tree
  (dirty or clean, which ref exists), and that dependency is confined to it: a bug or oddity in git
  ref resolution cannot affect `impact`, `used-by`, `nav-path`, or any other query.
- The map's metadata records a git commit "when there is one" — this is best-effort provenance for
  `meta.index.commit` (0009), not a requirement; its absence doesn't degrade any other command.

## Alternatives considered

- **Resolve constant users by shelling out to `git grep` (the pre-audit state).** This is what the
  code actually did until the audit found it; rejected as soon as found, both because it silently
  required a git repository to exist in a tool whose stated design is "git is optional", and because
  the `/src/main/` filter hard-coded one project's source-set layout (itself a 0006 violation).
- **Require git universally and drop the "no repository needed" requirement.** Never seriously
  considered: it was an explicit, early requirement from the project owner, independent of this
  specific bug, that the tool must not assume a git repository exists.
