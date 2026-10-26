# Architecture

This file is a map of the code: what the parts are, how data flows between them, and the rules that
are easy to break. The reasons behind the big choices are in [docs/decisions](docs/decisions). What
the tool gets wrong today is in [docs/limitations.md](docs/limitations.md).

## The idea

An Android project is compiled before it ships, and its compiled classes say exactly what calls what:
overloads, extension functions, lambdas, `invoke`, inlined code and Kotlin's generated bridges are
already resolved. codemap reads those classes plus the resources and manifests, builds a graph of
symbols, finds the screens in it, and walks the graph to answer:

- `impact`, `changes`: what may behave differently after a change, down to the screens a tester opens;
- `used-by`, `uses`, `find`: direct neighbours of a symbol;
- `nav-path`: how a user gets to a screen from the launcher;
- `graph`: what a screen opens;
- `screens`: the screens and their hosts.

## Data flow

```
codemap init ──Gradle (once)──> codemap.model.json   modules, variants, class/source/res dirs
                                      │
classes (*.class) ─┐                  ▼
resources, XML    ─┼──> build ──> snapshot (build/codemap/codemap.snap) ──> queries ──> text / --json
manifests         ─┘    (~3 s for ~35k classes)       mapped, ~50 ms to load
codemap.yml + presets ──> settings (stored in the snapshot, so queries use what build used)
```

`build` precomputes everything a query would otherwise redo: overriders, members, the hierarchy, and
who opens every screen (`opened_by`). Queries only load the snapshot and walk it.

## Packages

Each package imports only the ones above it in this list.

| Package | What it holds |
|---|---|
| `internal/util` | small helpers: sorted keys, sets, file checks, the user-error type |
| `internal/classfile` | the class file parser: members, references with their kind and line, call arguments that are constants, Kotlin metadata the walk needs (default-argument guards, `when` switch maps, signatures) |
| `internal/config` | `codemap.yml`, the built-in presets (`presets/*.yml`), the JSON Schema, and the settings derived from them (`applyConfig`) |
| `internal/project` | the modules of the project: `codemap init` (runs Gradle with `codemap.init.gradle`), `codemap.model.json`, the fallback discovery by class directories |
| `internal/android` | resources, manifests, layouts and navigation XML: what each resource is and what it references |
| `internal/snapshot` | the map's format: columns, CSR edges, the binary snapshot, derived tables (overriders, hierarchy), edge kinds |
| `internal/build` | classes + resources into a map: nodes, edges, screens, hosts, routes, navigation graphs |
| `internal/query` | the `Graph` over a loaded map, the walks (impact, opened_by, nav-path, graph), the stale check, the commands and their output |
| `cmd/codemap` | the CLI: argument dispatch, `build`, `init`, `config` |
| `internal/fixture` | tests only: a copy of `testdata/navapp` laid out as a built project |

## The map

**Nodes**: classes (`C`), methods (`M`), fields (`F`), resources (`R`, e.g. `@layout/main`,
`@id/action_x`) and routes (`ROUTE`, a Compose or Navigation 3 destination). Each node has an owner
(a member's class) and a top (the outermost class), a module, a file and a line range.

**Edges** (`internal/snapshot`): `CALL`, `READ`, `WRITE` (field access), `TYPE` (a type in a
signature, cast or class literal), `EXTENDS`, `IMPLEMENTS`, `LAMBDA` (a lambda or method reference
created here), `INLINE` (code inlined from an inline function), `RES` (code reads a resource),
`XML_RES` and `XML_CLASS` (XML references a resource or a class), `ROUTE_CONTENT` (a route and the
code that draws it), `ROUTE_HOST` (a route and the Activity that hosts it), `OPENS` (code or a
navigation resource opens a screen or a route), `INJECT` (a field whose generic type a class
implements: it can hold that class from DI).

Side tables keep what plain edges lose: `CallVia` (the class named at a call of an inherited method),
`CallSite` (constant arguments at each call), `DefaultGuard` (which references of a `$default`
bridge run only when an argument is left out), `ParamSwitch`/`DirectRead` (enum entries switched on),
`Annotation`, `ResDef`, `Entry` (launcher, deep link and intent filters from manifests).

**Screens**: Activities, Fragments and DialogFragments (by base class, from settings), Compose routes
(by registration calls, from settings), screen previews and screen layouts. Each screen gets its hosts
(the Activities it runs in): a route from the Activity that registers it, any other screen by walking
back over calls, type uses and resource references (code that creates a fragment, a layout or a
navigation graph that names it) to the Activities that reach it.

## build

`internal/build` parses every class file in parallel, then in stages: classes and members, edges,
resources, screens (routes, navigation graphs, entries, hosts). `cmd/codemap` writes the columns
with `Builder.Map`, runs `query.PrecomputeOpenedBy` (one Graph per worker) and writes the snapshot.
`build` does nothing when the map was built by the same binary from the same inputs (`--force`).

## impact

A breadth-first walk from the changed nodes over incoming edges (who depends on this). Each step
records its depth, its parent and why (`calls`, `reads`, `extends`, ...), so every answer has a path.

- **Modes.** A key is a node with a mode: `member` (this member changed), `whole` (everything in the
  class may change), `state` (instances behave differently but the members are the same, e.g. a
  constructor or a field initializer changed). A constructor does not widen to the whole class, so a
  change to one Activity's constructor does not reach its siblings.
- **Context.** A call through `this` in a base class runs on the subclass that the walk came from:
  the key carries the receiver class and the overridable methods it came up through. A subclass that
  overrides one of them is cut, so a change to `Base.onLogout()` does not reach screens whose class
  overrides `onLogout`.
- **Generic DI.** A call through a generic holder (`BaseFragment<VM>`, a `ViewModelProvider<VM>`) is
  narrowed to the classes whose generic signature binds that type.
- **Two passes.** The second pass blocks calls the first proved cannot reach the change: a `$default`
  bridge whose callers pass the argument, an enum entry switched on through a parameter that callers
  never pass.
- **Stops.** The walk stops at screens (a member of a screen class reports the screen), at routes, and
  does not walk through DI glue (Dagger/Hilt generated factories and components).
- **Constants.** Kotlin inlines `const val`, so their users come from the sources in the map (files
  that name the constant and import it or share its package).

## nav-path, opened_by, graph

`opened_by` (precomputed) lists, for each screen, the screens that open it and the way: the code
from the opener to the screen, the hub it went through (a navigator many screens share), and the
host it needs. `nav-path` is a Dijkstra walk over states (screen, host) from the target back to entry
screens (launcher, deep link, intent filter), with step costs: own code, shared code, push or deep
link handling (`nav_path.costs`). `--from`, `--entry` and `--avoid` change where the walk ends and
what it keeps off; `--via` chains walks, from the target to the last screen asked for and on to an
entry; `--ways` runs `--via` once per screen that opens the fork, the first screen back from the
target that several screens open ([0012](docs/decisions/0012-nav-path-via-and-ways.md)). `graph`
walks the other way, from a screen to what it opens (`--in`: back, to what opens it); `--to` keeps
the ways that reach a screen in time, by the fewest steps from each screen to it.

## Settings

`codemap.yml` extends built-in presets; lists add up, a few replace, scalars set later win. Unknown
keys are errors. Everything that differs between projects lives there: base classes, navigation
calls, the variant, class directories. The settings a map was built with are stored in it.

## Invariants

- Nothing project-specific in code. A new project's need is a setting or a preset.
- Gradle runs only in `codemap init`. `build` and the queries never start Gradle or reach the network;
  git is needed only by `changes` (`build` records HEAD when there is one).
- Output is deterministic: the same inputs give byte-identical `--json`. Never rely on map iteration
  order; ties are broken by node ids.
- The `--json` shape is a contract ([docs/json.md](docs/json.md)): change it deliberately, with
  `schema_version`.
- Answers are guarded by golden tests on `testdata/navapp`; a change that moves an answer updates the
  golden files in the same commit.
