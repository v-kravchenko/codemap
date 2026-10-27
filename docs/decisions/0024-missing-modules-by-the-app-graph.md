# 0024. Missing modules: those the app depends on

- Status: accepted
- Date: 2026-10-27

## Context

A module with sources and no class file of the variant is missing (`meta.index.missing_modules`),
and `--strict` fails. A test helper (`testImplementation` only) is never compiled by the app's
compile tasks, so a CI job that compiles the app and runs no unit tests never passed `--strict`,
though the map lacks nothing of the app. The build's hint named both compile tasks of every module
not compiled: about 120 for 60 modules, where the app's two compile them all.

## Options

- **A setting listing the modules to ignore.** Cheap, but a hand-kept list: a module the app
  starts depending on stays ignored and the map silently misses it.
- **The test modules by name or type.** A guess; a helper is an ordinary library module.
- **The module graph in the Gradle model.** The declared project dependencies of each variant's
  compile and runtime classpaths, read in the configuration phase (nothing resolved): what the app
  is made of is known, not guessed.

## Decision

The model. A module counts as missing only when an application or dynamic-feature module reaches it
through compile or runtime dependencies. The hint names the compile tasks of the first app reaching
a module through compile dependencies alone (compiling the app compiles it), else the module's own
(a runtime-only dependency). A model without dependencies (a hook's copy of an older script), or
without an app (a library project), counts every module as before.

## Consequences

- `missing_modules` keeps its shape; it can only shrink.
- The hook's copy of the script must be rewritten (`codemap wrapper`) for its model to have the
  dependencies; until then the old behavior.
- The graph is the declared one: a dependency added by a plugin outside the classpath
  configurations is not in it.
- The model has the module graph the ROADMAP's dependency queries need, without the classpath of
  libraries.
