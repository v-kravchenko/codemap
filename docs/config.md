# codemap.yml reference

`codemap.yml` lives at the project root, next to `settings.gradle`/`settings.gradle.kts` (the
directory `codemap` finds by walking up from the working directory, see `util.FindRoot`). Without
the file the tool runs as if it had `extends: [android, nav2-compose]`.

Unknown keys are errors (`codemap.schema.json`, `"additionalProperties": false`, and the YAML
decoder rejects unknown fields too).

```
codemap config check    # the file and its presets load, patterns compile
codemap config print    # the settings after the presets, as YAML
codemap config schema   # codemap.schema.json on stdout
```

For editor completion, save the schema once and point a yaml-language-server comment at it:

```
codemap config schema > codemap.schema.json
```

```yaml
# yaml-language-server: $schema=./codemap.schema.json
version: 1
```

## version

```yaml
version: 1
```

Required. The only accepted value is `1`.

## extends

```yaml
extends: [android, nav2-compose]
```

Type: array of strings, one of the built-in preset names below. Merged in the order listed, before
the file's own settings are merged on top (`internal/config/presets/*.yml`, `LoadConfig` /
`resolve`). The file's own `extends` is validated and kept in the loaded config as-is (not what a
preset itself extends).

| Preset | Adds | Use when |
|---|---|---|
| `android` | Activity/Fragment/DialogFragment base classes (AndroidX, AppCompat, Material, plain `android.app.Activity`); AGP 8/9 class dirs for `built_in_kotlinc` and `javac`; `main`/`{variant}` source sets; `changes.base: [origin/main, main]`; nav-path costs (`own:1 shared:3 push:30`, `branch_slack:4`, `path_limit:64`, `hub_min:10`); `graph.fanout:15`, `graph.max_edges:400` | Always — the default base screen/build settings for an Android app |
| `dagger-android` | Activity/Fragment/DialogFragment bases from the `dagger.android(.support)` package | The app still uses `dagger-android`'s own Activity/Fragment base classes |
| `nav-events` | `navigation.open`: emitting on a `MutableSharedFlow`/`MutableStateFlow`/`MutableLiveData` opens the route it carries | Automatically pulled in by every `nav2-compose*`/`nav3` preset — a ViewModel's one-shot navigation event, collected by the screen, counts as opening the route |
| `nav2-compose` | `navigation.typed_routes: true`; registers `composable<T>`/`dialog<T>`/the Compose `NavGraphBuilder` destination builders; `navigation<T>(startDestination = S)` and `NavGraphBuilder.<init>` as nested-graph registration; `navigate(Route)` and `NavHost` as open | Navigation Compose 2.8+ with type-safe (`@Serializable` class) routes |
| `nav2-compose-strings` | `navigation.string_routes: true`; registers `composable("…")`/`dialog("…")`; `navigation(...)` as nested-graph registration; `NavController.navigate`/`NavHostController.navigate`/`NavHost` as open | Navigation Compose with string routes (`"profile/{id}"`) |
| `nav3` | `navigation.typed_routes: true`; `route_interfaces: [androidx.navigation3.runtime.NavKey]`; `entryProvider`/`NavEntry.<init>` as registration; adding/setting an element of the back stack (or any call taking a `NavKey`) as open | Navigation 3 |

A project can extend more than one navigation preset at once (`testdata/navapp` extends `android,
nav2-compose, nav2-compose-strings, nav3` to exercise every style in one fixture); the `register`/
`open`/`graph` lists simply add up.

## Merge rules

- Lists **add up** (deduplicated, first-seen order) from every extended preset, in order, then the
  file's own list on top — **except** `build.class_dirs`, `build.source_sets` and `changes.base`,
  which **replace** (`orderedLists` in `config.go`): set any one of those three in your file and it
  throws away whatever the presets set, it does not append to it.
- Scalars (`variant`, `snapshot_dir`, `screens.layout_pattern`, `navigation.typed_routes`, …): the
  value set **later** wins (a later preset over an earlier one, the file over every preset).
- A nested object (`build`, `screens`, `navigation`, `nav_path.costs`, …) merges key by key, not as
  a whole: setting `nav_path.costs.push` does not reset `own`/`shared`.

## Keys

### variant

```yaml
variant: debug
```

Type: string. Default: `debug` (set by the `android` preset; if truly unset anywhere,
`ApplyConfig` falls back to `debug`). The Gradle build variant whose classes are read, e.g.
`debug`, `googleDebug`. Substituted into `build.class_dirs`/`build.source_sets` (`{variant}`),
names the map (`<variant>.snap` in `snapshot_dir`) and is read from `codemap.model.json`'s per-variant data (`codemap init` /
`project.ModelModules`). `codemap build --variant X` and a query's `--variant X` override it for one
run: the map of `release` sits next to the map of `debug`, and `meta.index.variant` says which one
answered.

### snapshot_dir

```yaml
snapshot_dir: build/codemap
```

Type: string. Default: `build/codemap`, relative to the root. The directory of the binary maps:
`codemap build` writes the map of a variant to `<variant>.snap` in it (`debug.snap`,
`release.snap`), and a query reads the one of its `--variant` or the configured `variant`
(`--out`/`--db` override per invocation). The file name is not configurable, so maps of two
variants never share a file. This and `variant` are the keys the queries read straight from the
YAML file before loading the map (`config.SnapshotPath`) — the map itself carries the rest of the
settings it was built with. The former `snapshot` key (a file path) is refused with a pointer here.

### build.class_dirs

```yaml
build:
  class_dirs:
    - build/intermediates/built_in_kotlinc/{variant}/compile{Variant}Kotlin/classes
    - build/intermediates/javac/{variant}/compile{Variant}JavaWithJavac/classes
```

Type: array of strings, **replaces** across the merge. Class directories of a module, relative to
it; `{variant}` and `{Variant}` (capitalized) are substituted. Used only when there is no
`codemap.model.json` (no `codemap init` yet, or it's stale): `build.DiscoverModules` walks the
project tree and treats any directory as a module if, for the current variant, at least one of
these globs resolves to an existing directory under it.

### build.source_sets

```yaml
build:
  source_sets: [main, "{variant}"]
```

Type: array of strings, **replaces**. Source sets read, in order (later entries override earlier
ones for the same resource/manifest key); `{variant}` is substituted. Used the same
walking-the-tree way as `class_dirs`, for resources/sources/manifests when there is no project
model.

### changes.base

```yaml
changes:
  base: [origin/main, main]
```

Type: array of strings, **replaces**. Git refs tried in order for the merge base of
`codemap changes` when `--base` is not given (`defaultBase`); the first one `git merge-base`
resolves against wins.

### screens.activity_bases / screens.fragment_bases / screens.dialog_bases

```yaml
screens:
  activity_bases: [androidx.appcompat.app.AppCompatActivity]
  fragment_bases: [androidx.fragment.app.Fragment]
  dialog_bases: [androidx.fragment.app.DialogFragment]
```

Type: array of dotted class names, add up. A class is an activity/fragment/dialog screen when it
(transitively) extends one of these. `dialog_bases` classes also count as fragment bases (a dialog
fragment is a fragment too).

### screens.preview_annotations

```yaml
screens:
  preview_annotations: [Preview]
```

Type: array of simple (unqualified) annotation names, add up. A function annotated with one of
these is a screen preview.

### screens.preview_note

```yaml
screens:
  preview_note: multiplatform
```

Type: string, later wins. Appended in parentheses to the "Screen previews" heading, e.g.
`Screen previews (multiplatform)`.

### screens.layout_pattern

```yaml
screens:
  layout_pattern: "^layout/screen_"
```

Type: string (regexp), later wins. Matched against `type/name` of a resource; a match marks it a
screen layout. Default: a pattern that matches nothing (`$^`) — no resource counts as a layout
unless you set this.

### screens.debug_source_sets

```yaml
screens:
  debug_source_sets: [debug]
```

Type: array of strings, add up. Source sets whose screens are debug-only (affects `graph`'s
debug-only filtering, `isDebug`).

### navigation.route_bases

```yaml
navigation:
  route_bases: [com.example.nav.BaseRoute]
```

Type: array of dotted class names, add up. Classes whose subclasses — Kotlin `object`s or data
classes — are routes (used alongside or instead of `route_interfaces` and `typed_routes`
registration).

### navigation.child_bases

```yaml
navigation:
  child_bases: [com.example.nav.ChildGraph]
```

Type: array of dotted class names, add up. Classes whose subclasses are nested navigation graphs.

### navigation.route_interfaces

```yaml
navigation:
  route_interfaces: [androidx.navigation3.runtime.NavKey]
```

Type: array of dotted interface names, add up. Interfaces that mark a route type (set by `nav3`
for `NavKey`).

### navigation.register

```yaml
navigation:
  register:
    - androidx.navigation.compose.NavGraphBuilderKt.composable
```

Type: array of `Class.member` globs, add up. Calls that register a destination: with
`typed_routes`, the class literal (or `is` check) passed to the call is the route; with
`string_routes`, the string constant passed is the route.

### navigation.open

```yaml
navigation:
  open:
    - androidx.navigation.NavController.navigate
```

Type: array of `Class.member` globs, add up. Calls that open a route handed to them: by name
(`navigation.open` match on a call that names a registered route), or — with `typed_routes`/
`string_routes` — the route object/constructor call or string/string-template handed to the call.

### navigation.graph

```yaml
navigation:
  graph:
    - androidx.navigation.compose.NavGraphBuilderKt.navigation
```

Type: array of `Class.member` globs, add up. Calls that register a nested graph from its start
route and its own route; opening the graph opens its start route (`navigation.start_destination`
names the method that returns it).

### navigation.not_opening

```yaml
navigation:
  not_opening: [copy, toString]
```

Type: array of name globs, add up. Member name globs of route-type methods that read a route
without opening it (`<init>` is always included). Prevents e.g. a route's own `copy()` from
counting as opening itself.

### navigation.start_destination

```yaml
navigation:
  start_destination: startDestination
```

Type: string, later wins. The method/parameter name of a graph-registration call that supplies its
start route (set by the `nav2-compose*` presets to `startDestination`).

### navigation.typed_routes

```yaml
navigation:
  typed_routes: true
```

Type: bool, later wins. Routes are classes: registered by the class literal (or `is` check) before
a `register` call; an object or constructor call handed to an `open` call, or returned/left in a
method, opens it. The route's own code, its sealed parent's code, and deep links built from a URI
at run time do not count as opening it.

### navigation.string_routes

```yaml
navigation:
  string_routes: true
```

Type: bool, later wins. Routes are strings: registered by the string constant before a `register`
call; `{arg}` placeholders and the parts of a string template match any path segment, `?query` is
left out. A route kept in a variable or passed as `Any` is not seen.

### navigation.hint_skip

```yaml
navigation:
  hint_skip: [invoke]
```

Type: array of method-name globs, add up. Method names never shown as the step hint of `nav-path`
(e.g. a generic `invoke` that is not informative).

### nav_path.push_markers

```yaml
nav_path:
  push_markers: [Push, DeepLink]
```

Type: array of strings, add up. Parts of method names that mark push/deep-link handling along a
`nav-path` way (a step whose code runs through one of these is a "push" step, costed at
`nav_path.costs.push`).

### nav_path.costs.own / nav_path.costs.shared / nav_path.costs.push

```yaml
nav_path:
  costs: { own: 1, shared: 3, push: 30 }
```

Type: integer, ≥ 1, default `own: 1`, `shared: 3`, `push: 30` (falls back to these when `0`/unset,
`ApplyConfig`'s `pick`). Cost of one `nav-path` step through the screen's own code (`own`), through
shared code such as a navigator many screens call (`shared`), or added for a step through push/deep
link handling (`push`, on top of `own`/`shared`).

### nav_path.branch_slack

```yaml
nav_path:
  branch_slack: 4
```

Type: integer, ≥ 1, default `4`. Other launcher branches are shown in `nav-path` when at most this
much dearer than the cheapest way.

### nav_path.path_limit

```yaml
nav_path:
  path_limit: 64
```

Type: integer, ≥ 1, default `64`. The shortest ways `opened_by` tries for one screen pair before
giving up (caps the search, not the output).

### nav_path.hub_min

```yaml
nav_path:
  hub_min: 10
```

Type: integer, ≥ 1, default `10`. Screens below a node that make it a "hub" (shared navigator code
drawn once instead of fanned out).

### graph.fanout

```yaml
graph:
  fanout: 15
```

Type: integer, ≥ 1, default `15`. Screens one hub opens before `graph` collapses the rest into a
`+N` ("more") node.

### graph.max_edges

```yaml
graph:
  max_edges: 400
```

Type: integer, ≥ 1, default `400`. Total edges `graph` draws before cutting the rest off
(`edges_left_out` in the JSON result counts what was cut).

## Class.member glob syntax

Class names are dotted (`com.example.Base`, `Outer$Inner` for a nested class). A
`navigation.register`/`navigation.open`/`navigation.graph` entry is `Class.member`: everything up
to the **last** dot is the class (its own dots are package separators), everything after is the
member. `*` in either part matches a run of word characters (`[A-Za-z0-9_]+`); a pattern
`name` also matches the Kotlin default-arguments synthetic `name$default`.

```
androidx.navigation.compose.NavGraphBuilderKt.composable   # exact class, exact member
com.example.nav.*.open*                                     # any class in the package, any "open…" member
androidx.navigation.NavGraphBuilder.<init>                   # constructor
```

## codemap init / codemap.model.json

`codemap init [--gradle CMD]` runs Gradle once (`help`, with the init script as an init script,
configuration phase only — nothing is compiled) and writes `codemap.model.json` at the root: the
Gradle modules with their path and directory, and for each build variant the compile tasks, class
directories, source sets, resource directories and manifests of its source sets, and the modules its
compile and runtime classpaths depend on (`compile_deps`, `runtime_deps`: Gradle paths, as declared).

With this model present, `codemap build` and the queries do **not** walk the tree or guess
directories: they take the modules of the configured `variant` straight from the model
(`project.ModelModules`), name any module whose classes are missing together with the Gradle tasks
that compile it, and refuse to build when the Gradle/version-catalog/settings files have changed
since `codemap init` ran (re-run `codemap init`). A module counts as missing only when an
application or dynamic-feature module depends on it (test dependencies left out), and the tasks
named are the app's when compiling the app compiles the module too (docs/decisions/0024); a model
written before the dependencies (a hook's older copy of the script: rerun `codemap wrapper`) counts
every module and names each one's tasks.

Without a `codemap.model.json` (or when it is missing a variant), the build instead discovers
modules by walking the project tree for directories containing `src/`, matching `build.class_dirs`
and `build.source_sets` from the config against each candidate.

`variant` and `{variant}`/`{Variant}` substitution are shared between `codemap.yml`'s
`build.class_dirs`/`build.source_sets` and the model's per-variant entries — the model simply makes
the lookup exact instead of a glob-like walk.

## Complete example (Compose + Fragments)

A typical mixed app: Jetpack Compose screens on Navigation Compose with type-safe routes, a legacy
section still in Fragments reached through an XML navigation graph, and a custom route base class.

```yaml
# yaml-language-server: $schema=./codemap.schema.json
version: 1
extends: [android, nav2-compose]

variant: debug
snapshot_dir: build/codemap

screens:
  activity_bases:
    - com.example.core.BaseActivity
  preview_annotations: [Preview, DevicePreview]
  preview_note: internal builds only
  layout_pattern: "^layout/screen_"
  debug_source_sets: [debug]

navigation:
  route_bases:
    - com.example.nav.BaseRoute
  register:
    - com.example.nav.NavGraphExtKt.register
  open:
    - com.example.nav.Navigator.open
    - com.example.nav.Navigator.openForResult
  graph:
    - com.example.nav.NavGraphExtKt.subGraph
  not_opening: [copy]
  hint_skip: [invoke]

nav_path:
  push_markers: [Push, Notification]
  costs: { own: 1, shared: 3, push: 30 }
  branch_slack: 4

graph:
  fanout: 12
  max_edges: 300

changes:
  base: [origin/develop, develop]
```

This extends `android` (base screen classes, AGP class dirs, default nav-path/graph numbers) and
`nav2-compose` (type-safe Compose routes, `composable<T>`/`navigation<T>`/`navigate(Route)`), then
adds: a custom Activity base (`com.example.core.BaseActivity`, in addition to the AndroidX ones
`android` already lists); a second preview annotation and a note shown on its heading; a layout
naming convention; a custom route base class and the project's own `Navigator.open`/`openForResult`
as the calls that open a route (on top of `NavController.navigate`/`NavHost` already registered by
`nav2-compose`); push-notification step costs; and `changes.base` pointing at this repo's trunk
branch instead of `origin/main`/`main`.

## Minimal example

```yaml
version: 1
```

Equivalent to having no `codemap.yml` at all: runs with `extends: [android, nav2-compose]` and
every other default.
