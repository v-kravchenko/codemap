# 0005. Gradle only in `codemap init`

- Status: accepted
- Date: 2026-10-25

## Context

codemap has to learn three facts about an arbitrary Android project before it can build a map: the
module list, each variant's compiled class directories, and each variant's source/resource/
manifest directories. These differ across Android Gradle Plugin versions, Kotlin compiler plugin
choices (the built-in Kotlin compiler vs. the separate `kotlin-android` plugin), build variants
and flavor dimensions, and whether a module even has compiled classes yet.

Three designs were tried in order, each discussed with the project owner before moving on:

1. **Guess it.** Parse `settings.gradle`, assume a fixed class-directory template per AGP
   generation, assume `main`/`debug`-style source sets. Fast and needs no Gradle at all, but the
   template breaks whenever the AGP or Kotlin plugin combination changes — it was hard-coded
   against one project's setup and simply did not start against a second, differently-configured
   project (a single-module app using the separate Kotlin plugin, with multiple product flavors).
2. **A mandatory Gradle build task**, wired into `build-logic`, that every CI build and every
   developer build would run, writing the module/class-directory/source-set description as a side
   effect of compiling. Exact, because it reads Android Gradle Plugin's own `Variant` API instead
   of guessing a path template. But it means `codemap` cannot work without a working Gradle
   invocation and a project willing to add a build-logic dependency, and it means every build (not
   just an on-demand "update the map" step) pays that cost or complexity.
3. **Gradle once, cached as a file.** A one-off `codemap init` command runs a Gradle init script,
   which inspects the Android Gradle Plugin `Variant` API without compiling anything, and writes
   the result to `codemap.model.json`. Every later `build` and every query reads that file and
   never touches Gradle again.

Design 2 was explicitly adopted first, then overturned: "no Gradle task; the CLI knows nothing
about Gradle, it is a standalone tool." Design 1 was then found to be too fragile across real
projects to rely on alone. Design 3 (the project owner's own proposal, called "variant C" in
discussion) is what shipped.

## Decision

`codemap init [--gradle CMD]` runs Gradle exactly once, with a configuration-only init script
(`help` plus the init script; no configuration cache, since a configuration-cache hit skips the
hook the script depends on), and writes `codemap.model.json`: every module's Gradle path, and for
every variant its compiled class directories (read from the compile tasks' configured outputs, not
guessed from a path template), its sources, resources, and manifests.

`codemap build` and every query command read `codemap.model.json` if it exists and never invoke
Gradle, start a daemon, or touch the network. `build` compares a hash of the project's build files
(settings, build scripts, version catalogs, `gradle.properties`, included builds) against the hash
recorded at `init` time, and refuses to use a stale model, printing "run codemap init" instead of
silently falling back to guessing.

Without a model file at all, the tool falls back to the original guess-based discovery (a
directory with both a `src/` and a known class-directory shape) as a reduced-functionality
fallback, not as the primary path.

## Consequences

- A `codemap init` run costs real wall-clock time (tens of seconds cold, single-digit seconds with
  a warm Gradle daemon) but only once per change to build configuration, not on every build or
  query.
- `codemap build` stays fast and offline: it cannot be slowed down or broken by Gradle, a build
  cache, or network state, because it never calls Gradle.
- A build file change invalidates the model explicitly (by hash) rather than silently; the model
  going stale is a user-visible instruction ("run codemap init"), not a guess that might be wrong.
- The guess-based fallback (design 1) still exists for the case of no model file, so the tool is
  not completely useless without ever running `init` — but it inherits that design's known
  fragility across AGP/Kotlin-plugin combinations, which is exactly why `init` exists.

## Alternatives considered

- **Guess the model from directory conventions alone (no Gradle, ever).** Rejected as the sole
  mechanism: a fixed class-directory and source-set template breaks across AGP/Kotlin-plugin
  combinations and does not generalize past the project it was first written against. Kept only as
  a fallback when no model file is present.
- **A mandatory Gradle task wired into every build.** Rejected: ties the tool's correctness to a
  working Gradle setup and a build-logic change in every consuming project, and runs on every
  build rather than only when the facts it produces can actually change (build-file edits).
