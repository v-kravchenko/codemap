# Limitations

What `codemap` does not see or gets wrong today, as of the current code. Each item names the
workaround if there is one. Items with an open fix are also in `ROADMAP.md`.

## Inputs not analysed

- The map is built from compiled `.class` files, resources and manifests (screens, launcher,
  deep link and intent filters, components). Gradle files (`build.gradle*`, `libs.versions.toml`,
  `gradle.properties`) and ProGuard/R8 rules are not read for behaviour: `codemap init` asks
  Gradle only where the modules, class directories, sources and resources are.
- One map holds one build variant (`codemap.yml`'s `variant:`, default `debug`; `--variant` for
  one run). A release-only code path, a flavor-specific override, or code gated by `BuildConfig`
  fields the variant does not take is invisible in its map. `codemap build --variant
  release` next to the debug map and `codemap diff --base build/codemap/release.snap` shows what
  only one of them has.
- A release map is built from the classes before R8: code R8 strips as unused is still in it; an
  obfuscated stack trace is read against it through the build's `mapping.txt` (`crash --mapping`).
- Reflection (`Class.forName`, method handles) and deep links resolved from a URI at run time
  are not followed — a destination reached only that way has no edge into it. Example: a
  screen registered solely through a `NavDeepLink` URI pattern shows no opener.
- No classpath of library `.jar`/`.aar` dependencies: edges stop at the project's own compiled
  classes, so a call into a library's internals (not just the library's public API your code
  calls) is invisible, and no query reads the module dependency graph.
- A component is a class of the map: a library's service or provider in a module's manifest
  (`FileProvider`) is not one, and a worker extends a base in `components.worker_bases` through
  the project's own classes only (a library base not listed there hides its workers). Of a
  component's intent filters only the push actions are read (0028), and `diff`, `nav-path` and
  `screens` do not list components (0027).

## Impact precision

- A non-abstract base `Activity`/`Fragment` is itself a screen (any concrete subclass of
  `android.app.Activity`/`androidx.fragment.app.Fragment` that isn't itself `abstract` or an
  interface counts), so the impact walk reaching that base class stops there instead of
  continuing into its subclasses. Example: `open class BaseFragment : Fragment()` used as a
  real, instantiated screen will absorb paths that should reach the fragments that extend it.
  Workaround: keep shared bases `abstract` (the test fixture `testdata/navapp` does this
  deliberately, see `BaseActivity`/`BaseVmFragment` in `impact/Overrides.kt` and
  `impact/GenericDi.kt`).
- A shared resource or constant (`@string`, `@color`, a `const val` many screens read) reaches
  every reader, whether or not the reader's behaviour depends on the value, so `impact` on it is
  wide. Workaround: run `impact` on the code that uses the value when you can.
- A preview goes with a screen by the composables both draw (`screens` `previews`): a preview
  drawing only what no screen of its module draws, or a screen built from composables of another
  module, has none; a screen's own component previews (in its module, drawn by it alone) count as
  its previews. A preview in a file of its own drawing a body several screens share goes with all
  of them.
- `changes` tells what a change adds and removes only from a map of the base (`--base-db`,
  docs/decisions/0022), so the base must be compiled too; without one the status and `removed`
  are `null`. A screen moved to another class name is removed and added, except a top-level Kotlin
  function moved to another file of its package (`moved_from`); two of one package, name and
  parameters moved at once, one also changing its parameters, or one in a facade renamed by
  `@file:JvmName` stay removed and added. `codemap diff` compares by node id: a move is a screen
  removed and one added there.
- Dead code (a reachable-looking branch nobody actually triggers, e.g. a `when` branch keyed on
  a string your code never constructs) is not detected — the walk cannot tell statically
  unreachable branches from live ones.

## Navigation / nav-path

- A destination that is reached only through a dynamic host — a `ViewPager`/`ViewPager2`
  adapter's `createFragment(position)`, or a `FragmentContainerView android:name="..."` set
  from code rather than declared in the layout — has no static edge from its host, so
  `nav-path` reports it as unreachable (`unknown`) or `outside` its real host.
- A string route kept in a variable, rebuilt with string concatenation beyond a simple
  template, or passed through as `Any`/an opaque parameter is not resolved; only a literal
  constant (or a `{placeholder}`/string-template segment) right before a `register`/`open`
  call is matched (`navigation.string_routes`).
- A destination registered only through a DI multibinding (e.g. a Hilt `@IntoSet` of navigation
  entries, a map of `NavKey` to content) has no static call edge from a registering class to a
  host Activity — `nav-path` reports "No way": there's a receiving list, but nothing shows
  which code populates and reaches it at run time.
- A route's `content` (`screens`) lists the repo composables called where the route is registered,
  so a helper composable called there is listed next to the screen; one that only a library draws
  is not listed, and without a LineNumberTable a wrapper's composables count too.
- A route can have more than one host (the same nested graph included from two different
  Activities); `nav-path` picks the cheapest path to one host, so a screen reachable in several
  different ways through different hosts shows only one of them unless you compare branches
  with `--verbose`.
- `nav-path --via` joins the cheapest part of the way between each two screens asked for; when a
  via screen has several hosts, the part from it on keeps the host the part before ended in, which
  is the cheapest way through that state, not always the cheapest way through the screen.
- `nav-path --ways` finds the fork on the cheapest way only: openers of a screen past the fork, or on
  a costlier way, show up only through `--via`.
- Back-navigation and results (`popBackStack`, `Activity.finish`, `setResult`, a
  `NavResultItem`-style callback) are not modeled at all; `nav-path` only answers how to reach
  a screen going forward, never what a screen returns to.
- A screen opened only from a debug-only source set, or only reachable in the IDE/build
  variant other than the one analysed, is not distinguished from a dead screen beyond the
  `debug_source_sets` marker already applied to screen registration.

## Graph

- `graph --in` does not follow hosts: it draws every way that opens a screen, also one that runs
  only in a host the way it is drawn from never goes through (the forward `graph` leaves those out
  and counts them in `ways_of_other_hosts`).
- `graph --to` keeps an edge when its screen reaches the target in the steps left, by the fewest
  steps over all ways; a host check further on can still end that way before the target.
- A navigator method that opens two or more screens depending on runtime state (a `when` over
  an app state) is shown as a single node with multiple outgoing edges; `graph` does not know
  which state picks which screen, and it does not separate the branches by which UI button
  triggered the call — a method handling several buttons in one `when` fans out as if it were
  one choice.
- A shared hub (a navigator class many screens call into) collapses once it has more outgoing
  edges than `graph.fanout` (default 15) into a `+N screens` marker instead of individual
  arrows; the full list is still in `--json`.
- The rendered graph is capped at `graph.max_edges` (default 400) arrows; a wide `--depth`
  from a large hub truncates silently past that, in the text output (JSON always has the full,
  uncapped edge list for what was walked).
- The "known modules/hosts" heuristic that labels a step "in <module>" (so an impossible
  cross-host jump isn't shown as a direct arrow) takes a screen's host from whichever single
  path first reached it during the walk, not from every possible host of that screen.
- `screens --unreachable` runs one `nav-path` walk back from each screen (about 3 s for 1,600
  screens); a screen opened only by reflection, a library or a framework callback the map does not
  see is listed as unreachable, with the end of its way (`unknown`, `outside`) saying why; one
  opened through a base class counts as reached even when no screen extending it is.

## Crash traces

- `crash` names the screens by the own frame reaching the fewest (0029). A trace whose own code
  starts in a coroutine or a callback another class posted (`Handler`, a `Flow` collector) has no
  frame of the screen that launched it: a shared helper there reaches every screen using it (806
  on a 78-module project for a logout coroutine), honest but wide.
- A frame is found by its class and the method whose lines hold its line, else by its name: code
  kotlinc inlined reports the inline function's file and line, so such a frame gets its method by
  name only, and a method name several overloads share gives the first.
- R8 mapping: inline chains, renamed classes and members and line ranges are read; R8's outlines,
  synthesized frames to drop (`com.android.tools.r8.rewriteFrame`) and residual signatures are
  not, so an outlined frame stays as R8 named it (`library`).
- Only the crashing thread is read; the other threads of a dump and `Suppressed` exceptions are
  skipped. An ANR's main-thread trace is read as any trace.

## Build & config

- Settings (`internal/config`) are package-level globals set once by `applyConfig`; there is
  no way to hold two different configs (e.g. two variants, or a project and a lookup project)
  loaded in the same process, which also means the config can't easily be made per-goroutine
  if a future command wanted to analyse several variants in one run.
- `internal/query` is a single ~7,500-line package with every query (`used-by`, `impact`,
  `changes`, `nav-path`, `graph`, `screens`) implemented as a method on one `*Graph` type; each
  command has a file of its own, but there is no sub-package boundary between, say, the nav-path
  walk and the impact walk, so a change to shared helpers can affect every command at once.
- `codemap init`'s project model (`codemap.model.json`) has the modules, class directories, source
  sets, resources, manifests and the declared project dependencies of each variant (0024), but not
  the resolved classpath of library dependencies, so nothing can tell which library class backs an
  interface; and only the missing modules read the module graph, no query answers which modules a
  change ripples through.

## Tooling

- The build hook (`gradle/codemap.gradle`) reaches every project with `allprojects`, which
  Gradle's Isolated Projects forbids: a build with it on fails to configure. Workaround: leave the
  hook out and run `codemap init` and `codemap build`; the fix is a settings plugin (0018).
- The hook rebuilds the whole map of a variant after any compile of it, even of one module
  (~3 s on 80 modules); there is no incremental build of the map.
- There is no `export` of the map to a dependency-free, queryable format (the map is read only
  by the CLI's own binary snapshot reader); ad-hoc exploration needs a query flag that already
  exists or a new one, not arbitrary SQL/joins over the map.
