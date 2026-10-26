# 0001. Analyse compiled classes, not source

- Status: accepted
- Date: 2026-10-22

## Context

The project needed a way to answer "what does changing this symbol affect" and "who uses this"
over a large Android app (~35k classes, ~1600 screens). Before building anything new, the
available source-level tools were tried on the same real changes.

Eight real changes with known traps were used as a benchmark: an operator-`invoke` call, an
extension function that shares a name with something else, an interface implementation, same-
package access without an import, a Hilt repository reached through a view model into a screen, a
string resource, a class name that collides with another in a different feature, and a view model
reached through a generic base class. The expected answers (which screens are affected) were
worked out by hand for each.

Four tools were measured against this benchmark: a general-purpose tree-sitter-based code graph
tool, a regex-based file-level dependency script, a shell script that walks `import`s one step,
and a prototype that reads compiled `.class` files directly.

## Decision

Build the map from compiled bytecode (`.class` files) and from resources/manifests/navigation XML,
not from parsing source with a grammar or with regular expressions, and not by shelling out to a
general-purpose source code graph tool.

## Consequences

- Correctness depends on type resolution the JVM already did at compile time: overload resolution
  by descriptor, `invoke` on a functional interface, a Kotlin extension compiled as a static call
  on a synthetic `*Kt` class, a Hilt constructor parameter as a real type edge. None of this has to
  be re-derived by guessing from syntax.
  Known bytecode features used for this: SMAP line mappings and `$i$f$name` locals give real
  inline-call sites; `R.string` references compiled to inlined `getstatic` (as AGP does in library
  modules) are still visible as the string's bytecode location.
- A full graph build over the whole app's classes takes single-digit seconds; a query against the
  built map answers in milliseconds.
- What is not visible this way: `const val` (inlined into every call site, no bytecode reference —
  resolved separately by scanning the map's own recorded source files, not by re-running a general
  text search tool); XML by itself (handled as its own input, not inferred from bytecode); `R`
  constants under `:app`-style modules, which get inlined at the final app module; symbols that
  were deleted outright (no class to read).
- The tool must read what the compiler already produced, so it only ever sees what was last
  compiled; a stale build gives a stale (but explicitly flagged) answer (see 0005).

## Alternatives considered

- **A general-purpose source code graph tool (tree-sitter, no type resolution).** Resolved calls
  by name, not by import or by type of the receiver, so two unrelated classes with the same method
  name produced both missed edges and false ones. Had no edge for a constructor-injected field, no
  edge from a multi-line class header in the project's formatting style (a parser miscounted a
  class's own supertype), no edge through an interface without blowing up combinatorially, and did
  not index XML or resources at all. Recall on the benchmark: 7-9 of 19 Activity/Fragment hosts, 4-5
  of 10 Compose routes, 8-9 of 21 preview/layout files, with up to several hundred false positives
  per case. Shipped with telemetry on by default.
- **A regex-based file-level dependency graph (an earlier prototype).** Best recall among the
  source-level options (14/19, 7/10, 12/21) because it worked at file granularity: import, same
  package, resource/attribute references, classes named in XML. But file granularity means a
  change to one method marks every user of the whole file as affected (up to 117 false positives
  on one case); it had no concept of a screen as a unit, so results were file groups, not a
  Fragment paired with its layout or a route paired with its host Activity; and it shared the
  bytecode prototype's win on resources but lost the Hilt-chain case entirely to the same
  multi-line-header parsing bug.
- **A one-hop `import`-walking script (the existing CI screenshot-diffing tool).** Only follows a
  single step from each changed file's imports, so any chain two or more calls deep (use case to
  repository to view model to screen) was invisible; it also builds its change template from every
  top-level declaration in a changed file, which pulled in unrelated declarations that happen to
  share a file.
- **Offline whole-program static analysis (a CPG-style tool).** Building a code property graph
  without a resolved classpath took over eight minutes and several gigabytes of memory for this
  codebase; stopped as impractical for a tool meant to run on every change.
