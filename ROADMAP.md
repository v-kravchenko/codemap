# Roadmap

Open requirements and ideas, prioritized. Once this repo is on GitHub, these items move to
GitHub Issues and this file becomes a pointer to the issue tracker instead of the backlog
itself.

## Next

- **Stack trace input, with R8/ProGuard `mapping.txt`.** Feed a crash's stack trace (from a
  debug or release build) and get back the affected screens/zones, the same way `impact`
  does for a symbol name. Without `mapping.txt` support, an obfuscated release stack trace
  can't be mapped back to real class/method names at all, so this is useless for the builds
  that actually crash in production. Screens alone are not the answer: a crash in
  `Application.onCreate`, a `Service`, a `BroadcastReceiver` or a `Worker` has no screen, so the
  answer leads with where it crashed (deepest own frame, its module and zone) and the entry
  point (lowest own frame), with screens only when the entry point reaches one. Naming those
  entry points needs manifest components and worker bases as a kind of node in the map.
- **Zones, with owners and a sensitive-zone exit code.** Group packages into named zones (e.g.
  "payments", "auth") with an owning team, a `zones check` that fails CI when a package has no
  zone or zones overlap, and a distinct exit code when a change touches a zone marked
  sensitive — so a security- or compliance-relevant change can be gated in CI the same way a
  test failure is, instead of relying on a reviewer noticing.
- **Renderers: Markdown and CI labels, applied idempotently.** A Markdown renderer for an MR
  comment and a label renderer for GitLab, both driven by the existing stable `--json`; label
  application must remove labels from a previous run that no longer apply (diff
  `--current` against the new set) so a merge request doesn't accumulate stale `zone::*`
  labels from earlier pushes.
- **`--explain`.** Show why a specific affected screen/zone was included — the full edge chain
  from the change to that screen, not the collapsed one `impact`/`changes` prints by default.
  Needed for anyone to trust a CI gate built on this data; without it, a false positive erodes
  confidence in the tool silently.
- **More fixture scenarios in `testdata/navapp`.** Golden tests currently cover one screen set
  per navigation preset plus XML nav graphs/dialogs/Activity intents; still missing: a
  dagger-android screen base (abstract base + generated `Dagger*`/`*_MembersInjector` glue, to
  regression-test the DI-glue filter beyond unit tests), a custom non-built-in base route class
  (to prove `route_bases`/`child_bases` config generalizes beyond the built-in presets), a
  `LiveData`-events navigation path (the `nav-events` preset exists but has no dedicated
  fixture), and a `ViewPager`/`ViewPager2` host (`createFragment`) to make the known gap
  (see `docs/limitations.md`) visible as a documented "No way"/`outside` case rather than a
  silent miss.

## Later

- **ViewPager/`FragmentContainerView` dynamic hosts as screen hosts.** Recognize
  `adapter.createFragment(position)` and a code-set `FragmentContainerView android:name` as
  valid hosts in `nav-path`/`impact --nav`, closing one of the two known navigation gaps
  (the other is DI-multibinding routes, below). Not prioritized yet because XML-declared,
  static hosting is far more common across real projects and already works.
- **A narrower impact for a widely-implemented, non-generic base contract** (many classes
  implement the same interface/base class, and the true caller only reaches one concrete
  implementation per call site through a DI binding that isn't expressed as a Java generic).
  The generic-DI narrowing already handles the `Base<VM>`-style case; this is the same idea
  for a plain (non-generic) shared contract with many implementations bound through DI, which
  today still fans out to every implementation.
- **A narrower impact for a call that only reaches one instance through a framework
  singleton** (e.g. `Application.getInstance()` read from a base class's lifecycle callback) —
  today it fans out to every screen reachable from that lifecycle callback, which is correct
  but maximally unhelpful.
- **Push as a nav-path entry (`--entry push`).** A `Service` whose intent filter takes a push
  (`com.google.firebase.MESSAGING_EVENT`, in a preset) becomes an entry, and the screens its code
  opens get `entry: push` instead of `outside`. Needs manifest components in the map, the same gap
  as the stack trace input.
- **nav-path through a DI multibinding** (a Hilt `@IntoSet`/map-multibinding of navigation
  destinations, a Navigation-3-style scene registry) — currently reported as "No way" because
  there's no static call edge from the registering code to the host that reads the set/map at
  run time.
- **String routes kept in a variable, rebuilt beyond a simple template, or passed as `Any`.**
  Only a literal constant or a straightforward string template right before a
  register/open call resolves today; broadening this needs real data-flow tracking, not just
  a wider regex.
- **Stale-check on `codemap.yml` changes.** A query currently doesn't warn when the config a
  map was built with differs from the one on disk; `build` already re-resolves correctly, this
  is only about catching a stale map at query time.
- **Classpath of library dependencies and a module dependency graph.** Needed before a call
  into a library's internals, or a "which modules does this change ripple through" query, can
  be answered; `codemap init`'s Gradle model has the declared project dependencies of each
  variant (0024, used for the missing modules), not yet the resolved classpath, and no query
  reads the module graph.
- **Split `internal/query` into sub-packages** (one per command: impact, nav-path, graph,
  changes): it's ~7,500 lines all on one `*Graph` type, a file per command since the split of
  cli.go and nav.go; packages would mean exporting the ~80 `Graph` helpers the commands share —
  tech debt, not a feature.
- **Less memory in `build`.** A 34,500-class project builds in 3.2s at 2.4 GB peak RSS for an
  82 MB map; the profile shows small map and slice churn (map hashing, memmove, GC), no single
  hotspot. Fine on a developer machine; worth trimming (release per-class structures sooner, fewer
  string-keyed maps) before CI runners with little memory or several builds at once.
- **Move config off package-level globals** (`internal/config`'s `applyConfig`-set package
  vars) to a value threaded through `Graph`/`Builder` — needed before two configs (two
  variants, or two projects) could ever be loaded in one process; not needed for anything on
  Next/Later today, so it's a Later-priority cleanup.

## Ideas

- **MCP wrapper over the stable CLI.** Expose `used-by`/`impact`/`nav-path`/`graph`/`changes`
  as MCP tools for an AI agent, as a thin adapter over the existing `--json` contract — no
  changes to the CLI itself, which is exactly the point of keeping the JSON contract stable.
- **`zones export`.** A `zone -> screens -> packages -> owners` catalog for onboarding, a
  portal, or a TMS — depends on zones (Next) existing first.
- **A dependency-free NDJSON export of the map.** The binary snapshot is only readable by the
  CLI's own code today; an NDJSON export of nodes/edges would let someone run ad-hoc queries
  with something like DuckDB without needing a SQLite export (removed earlier) or a new CLI
  flag for every new question.
- **CI index cache keyed by commit SHA.** A merge request in CI currently rebuilds the map from
  scratch; caching the built map by the commit it was built from would make incremental CI runs
  (base already built, only head changes) much cheaper.
- **Per-zone "widely affected" threshold** (in `codemap.yml`), so a
  CI gate can flag "this change reaches an unusually large fraction of a zone" rather than only
  "this change reaches zone X at all" — depends on zones (Next) existing first.
