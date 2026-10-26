# Limitations

What `codemap` does not see or gets wrong today, as of the current code. Each item names the
workaround if there is one. Items with an open fix are also in `ROADMAP.md`.

## Inputs not analysed

- The map is built from compiled `.class` files, resources and manifests (screens, launcher,
  deep link and intent filters). Gradle files (`build.gradle*`, `libs.versions.toml`,
  `gradle.properties`) and ProGuard/R8 rules are not read for behaviour: `codemap init` asks
  Gradle only where the modules, class directories, sources and resources are.
- Only one build variant is analysed at a time (`codemap.yml`'s `variant:`, default `debug`).
  A release-only code path, a flavor-specific override, or code gated by `BuildConfig` fields
  the configured variant does not take is invisible. Workaround: rebuild with a different
  `variant` (e.g. `release`) and compare.
- Reflection (`Class.forName`, method handles) and deep links resolved from a URI at run time
  are not followed — a destination reached only that way has no edge into it. Example: a
  screen registered solely through a `NavDeepLink` URI pattern shows no opener.
- No classpath of library `.jar`/`.aar` dependencies: edges stop at the project's own compiled
  classes, so a call into a library's internals (not just the library's public API your code
  calls) is invisible, and there is no module dependency graph.
- Changing `codemap.yml` itself does not mark the map stale for queries — only `build` re-reads
  it. A query against a map built under an old config silently keeps the old settings.

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

- `graph` only walks forward (what a screen opens); there is no "who can reach this screen"
  direction — use `nav-path`/`impact --nav` for that instead.
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

## Build & config

- Settings (`internal/config`) are package-level globals set once by `applyConfig`; there is
  no way to hold two different configs (e.g. two variants, or a project and a lookup project)
  loaded in the same process, which also means the config can't easily be made per-goroutine
  if a future command wanted to analyse several variants in one run.
- `internal/query` is a single ~7,000-line package with every query (`used-by`, `impact`,
  `changes`, `nav-path`, `graph`, `screens`) implemented as a method on one `*Graph` type; there
  is no sub-package boundary between, say, the nav-path walk and the impact walk, so a change
  to shared helpers can affect every command at once.
- `codemap init`'s project model (`codemap.model.json`) only tracks what Gradle reports for
  modules, class directories, source sets, resources and manifests; it does not capture the
  module dependency graph or the classpath of library dependencies, so nothing downstream can
  reason about which module depends on which, or which library class backs an interface.

## Tooling

- No CI pipeline: `gofmt`/`go vet`/`go test`/lint only run locally, on request.
- No tagged releases or prebuilt binaries; `go install`/`go build` from source is the only way
  to get the CLI today.
- There is no `export` of the map to a dependency-free, queryable format (the map is read only
  by the CLI's own binary snapshot reader); ad-hoc exploration needs a query flag that already
  exists or a new one, not arbitrary SQL/joins over the map.
