# --json contract

Every query command (`used-by`, `uses`, `impact`, `changes`, `diff`, `crash`, `nav-path`, `graph`,
`screens`, `find`, `build --info`) accepts `--json` and writes one JSON object to stdout, indented two spaces,
with keys in a fixed order (`internal/query/json.go`: `writeJSON`, object keys keep insertion
order, never alphabetized by a generic marshaler). The examples below are trimmed from
`testdata/golden/*.json`, which hold the exact output **without** the `meta` block (the golden test
strips it since `meta.run` is never reproducible).

## Envelope

```json
{
  "schema_version": "4.4.0",
  "command": "impact",
  "meta": {
    "tool_version": "0.3.0",
    "index": {
      "version": "...",
      "variant": "debug",
      "commit": "a1b2c3d...",
      "dirty": false,
      "inputs_sha1": "...",
      "stale": false,
      "stale_inputs": null,
      "uncompiled": [],
      "missing_modules": []
    },
    "refs": null,
    "run": { "started_at": "2024-01-01T00:00:00Z", "duration_s": 0.42 }
  },
  "...command-specific fields...": "..."
}
```

- `schema_version` — the shape of the JSON output (`"4.4.0"`, `internal/query/cli.go`). Independent
  of `tool_version`.
- `command` — the command name that produced the result (`"used-by"`, `"uses"`, `"impact"`,
  `"changes"`, `"diff"`, `"crash"`, `"nav-path"`, `"graph"`, `"screens"`, `"find"`, `"build"`).
- `meta.tool_version` — the codemap binary's own version (`"0.3.0"`, `codemap version`): the release's
  tag without `v`, `dev` for a build from a checkout.
- `meta.index.version` — the snapshot format version the map was built with (`index_version`);
  queries refuse to run against a map from a different one (see Exit codes).
- `meta.index.variant` — the build variant whose classes the map was built from (`codemap build
  --variant`, else `variant` in codemap.yml); since 3.6.0.
- `meta.index.commit` — the git HEAD the map was built from, or `null` when the project has no git
  repo or HEAD could not be read.
- `meta.index.dirty` — `true` when the working tree had uncommitted changes at build time (`git
  status --porcelain` was non-empty).
- `meta.index.inputs_sha1` — hash of every input (class files + watched source/resource files) the
  map was built from.
- `meta.index.stale` — `true`/`false` once the stale check ran (always runs unless `--no-check` or
  the command is `build`), `null` when the check did not run.
- `meta.index.stale_inputs` — `null` when not stale; otherwise an object: `sources`, `resources`,
  `classes` (counts of changed/added/removed inputs of each kind), `config` (bool: codemap.yml with
  its presets gives other settings than the map was built with, for the map's variant; since
  4.2.0), `compile` (modules with changed sources but no changed class files — not yet
  recompiled), `affects_answer` (bool: a changed file is actually used by this answer, or `config`:
  other settings may change any answer), `changed_files` (the changed files this answer uses).
- `meta.index.uncompiled` — the modules (directories) whose class directories hold no class file
  now, sorted, `null` when the check did not run: not compiled where the query runs (a fresh
  checkout of the map's commit, a clean), so the map is checked by their sources, resources and
  manifests alone, and their class files are not counted in `stale_inputs`; since 4.4.0.
- `meta.index.missing_modules` — the modules of the project model (`codemap init` or the build
  hook) with sources and no class file of the variant when the map was built, sorted: not
  compiled, so every answer may leave out what is in them; `[]` for a whole map and without a model.
  With the dependencies in the model, only the modules an application or dynamic-feature module
  depends on (transitively, test dependencies left out) count: a test helper misses nothing.
  A query warns of them on stderr; since 3.7.0.
- `meta.refs` — `null` for most commands; for `changes`, the resolved input/base/head (see
  `changes` below).
- `meta.run.started_at` — UTC timestamp the process started, `started_at` format
  `2006-01-02T15:04:05Z`.
- `meta.run.duration_s` — wall time since process start, seconds, rounded to 2 decimals.

## Determinism

The same inputs (code map + arguments) give byte-identical `--json`, apart from
`meta.run.started_at`/`duration_s` which are always wall-clock. Every list is explicitly sorted
(module, then symbol, then location, or the command's own documented order) — nothing relies on Go
map iteration order. A change to the shape of the output (new/removed/renamed field, reordered
array semantics) is a deliberate, documented change and bumps `schema_version`
(`internal/query/cli.go`, `AGENTS.md`: "a change to the --json shape is a deliberate, documented
change").

## Exit codes

| Code | Meaning |
|---|---|
| `0` | OK |
| `2` | A user error: bad arguments (an option the command does not take, an extra positional), symbol not found, ambiguous symbol, no code map at the given path, map from a different tool version (`index_version` mismatch), Gradle/git failures, etc. (`util.UserError`, caught in `Query`'s deferred recover) |
| `3` | `--strict` only: the code map misses modules not compiled (`meta.index.missing_modules`), or it is stale (older than its inputs) **and** this particular answer actually uses a changed file (`exitStale`, `internal/query/stale.go`). Without `--strict`, staleness is a warning on stderr and the exit code is whatever the command would otherwise return. |

With `--strict`, stdout is buffered and only flushed if the final code is not `3` — a strict,
stale run that would exit 3 prints nothing to stdout, only the warning/error to stderr.

## Staleness and --strict

Every query (not `build` without `--info`, and not with `--no-check`) compares the map's recorded
inputs against the filesystem: changed/added/removed `.class` files under the map's class
directories and those of its missing modules (compiled since the build), changed/added/removed
watched source/resource files, and the settings codemap.yml with its presets gives now against
those the map was built with (`stale_inputs.config`; other settings may change any answer, so with
`--strict` that exits `3` whatever the answer). If anything differs:

- Without `--strict`: a warning goes to stderr (which files changed, by kind, and the fix — compile
  the affected modules then `codemap build`, or just `codemap build` if no source needs
  recompiling); the command's answer is still printed, and `meta.index.stale`/`stale_inputs`
  reflect it.
- With `--strict`: if the stale files are actually walked into this particular answer
  (`stale_inputs.affects_answer`), the command exits `3` and prints nothing but the error. If the
  stale files are unrelated to this answer, it behaves like the non-strict case (warning only,
  normal exit code).

A map with missing modules (`meta.index.missing_modules`) answers without them: every query warns
(`the code map misses N modules (not compiled): ...`), and with `--strict` exits `3` whatever the
answer, since what it leaves out cannot be told from what it has. `build` names them in its last
line (`code map: 31 of 78 modules compiled, ...`) and in `build --info`.

A class directory of the map with no class file at all is not compiled, not changed
(`meta.index.uncompiled`): a map used from a fresh checkout of its commit, before or without a
compile, is up to date when the sources, resources and manifests are the map's, and stale with the
modules to compile when one of them differs. A directory that lost some of its class files is a
change as before.

`codemap build` itself does nothing (no-op, logged to stderr) when `UpToDate` finds no stale
inputs and `--force` was not given.

## Module globs

`--module GLOB` (repeatable; `impact`, `changes`, `used-by`, `uses`, `screens`, `find`, `graph`)
takes module paths the way `.gitignore` takes directories
([0013](decisions/0013-module-globs.md)):

- a glob takes the module and the modules under it: `feature/userdoc` is also
  `feature/userdoc/financial`, not `feature/userdocx`;
- `*` is a run within one path segment, `**` any run of segments (`**/impl`, `feature/**`);
- `!GLOB` takes back what the globs before it took; the last glob matching a module decides, and a
  module no glob matches is kept only when the first glob is a `!` one: `--module feature/userdoc
  --module '!feature/userdoc/*'` is the module alone, `--module '!feature/debug'` everything else;
- the Gradle path is the same module: `:feature:userdoc` is `feature/userdoc`;
- a glob that matches no module of the code map is an error (exit 2), not an empty answer.

Except in `graph`, `--module` filters the answer, not the walk: a change in `core/network` still
reaches a screen in `feature/cards` through `core/domain`. What it left out is counted in
`hidden_by_module`, an object present only with `--module`, of the lists it shortens.

## used-by / uses

```
codemap used-by|uses SYMBOL [--all] [--no-self] [--no-di] [--module GLOB]... [--json]
```

- `SYMBOL` (positional, required) — a class, method or field name; dotted, may be unqualified
  (`"DefaultsKt.defaultLabel"` or just `"defaultLabel"` if unambiguous).
- `--all` — do not fail on an ambiguous match across unrelated top-level classes; return/report all
  of them.
- `--no-self` (`used-by` only) — drop edges from the same top-level class as the match itself.
- `--no-di` (`used-by` only) — drop items that are generated DI glue (Hilt/Dagger factories,
  components, modules); `generated_di` still counts them.
- `--module GLOB` — only items in a matching module (see Module globs).
- `--json`

Result fields:
- `symbols` — the resolved symbol(s) matched, qualified, sorted.
- `items` — array of `{symbol, kind, module, location, edges}`: `kind` is one of `class`, `method`,
  `field`, `resource`, `route`; `edges` is the sorted set of edge-kind labels this item is connected
  through (e.g. `["call"]`, or `"<kind> <via-member>"` when the edge passes through a member other
  than the resolved symbol, with `" (generated DI)"` appended when the user is Hilt/Dagger-generated
  glue).
- `generated_di` — count of items that are generated DI glue (left out of `items` with `--no-di`).
- `hidden_by_module` — with `--module` only: `{items}`, the items left out.

```json
{
  "schema_version": "4.4.0",
  "command": "used-by",
  "symbols": ["navapp.impact.DefaultsKt.defaultLabel"],
  "items": [
    {
      "symbol": "navapp.impact.DefaultsKt.copyText",
      "kind": "method",
      "module": "app",
      "location": "app/src/main/java/navapp/impact/Defaults.kt:10",
      "edges": ["call"]
    }
  ],
  "generated_di": 0
}
```

## impact

```
codemap impact SYMBOL... [--depth N] [--kind KIND] [--no-paths] [--to TARGET] [--nav] [--module GLOB]... [--all] [--json]
```

- `SYMBOL...` (one or more, required) — the changed symbol(s); each resolved the same way as
  `used-by`'s `SYMBOL`.
- `--depth N` — cap the walk depth (default: unlimited).
- `--kind KIND` — restrict which screen kinds are listed (repeatable; `all` lists every kind;
  valid kinds come from the screen titles: `activity`, `fragment`, `dialog`, `route`, `preview`,
  `layout`). Default: a fixed subset of "real" screen kinds (`screenKinds`).
- `--no-paths` — omit the dependency path in the text output (ignored for `--json`, which always
  includes `path`).
- `--to TARGET` — instead of the full affected set, report only the shortest path from the changed
  symbol(s) to a named screen or symbol (see "impact --to" below).
- `--nav` — for each affected screen, also add how the user reaches it (same computation as
  `nav-path`'s cheapest way), as a `nav` sub-object.
- `--module GLOB` — only screens and affected symbols in a matching module (see Module globs); the
  walk is whole, `changed` is not filtered. Not with `--to`.
- `--all` — as in `used-by`.

Result fields (full `impact`, no `--to`):
- `changed` — the resolved seed symbols, as `{symbol, kind, module, location}`, sorted.
- `affected` — every affected symbol **except** the seeds and screens already listed in `screens`,
  as `{symbol, kind, module, location, depth, path}`; `depth` is the walk distance, `path` the
  qualified names from a changed symbol down to this one.
- `screens` — affected screens, as `{kind, name, target, module, location, host, hosts, depth,
  opened_from, previews, path[, nav]}`. `host`/`hosts`: the Activity/parent hosting a fragment/dialog (`host`
  is `hosts[0]` or `null`). `opened_from`: for a dialog, the screens observed to open it (by name);
  empty if none found. `previews`: as in `screens` (since 3.7.0); `--no-preview` keeps only the
  activities, fragments, dialogs and routes with none. `nav`: present only with `--nav`, `{entry, detail, cost, path}` (see
  `nav-path`) or `null` if no way was found.
- `components` — affected components, as `{kind, name, target, module, location, depth, path}`
  (since 4.2.0): `kind` is `application`, `service`, `receiver`, `provider` (a class the manifest
  of a module declares) or `worker` (a concrete class below `components.worker_bases`); `path` as
  in `screens`. A component does not stop the walk the way a screen does, so what uses it (an
  Application's getters) is in `screens` and `affected` too; `--kind` and `--no-preview` do not
  apply. docs/decisions/0027.
- `hidden` — counts of screens hidden by `--kind`, by kind.
- `modules` — count of affected symbols by module.
- `packages` — count of affected symbols by package.
- `not_covered` — (only non-empty for `changes`, always present for `impact`) list of
  `{path, reason}` for inputs that could not be analysed; `reason` is one of `deleted`,
  `test_source`, `release_only`, `not_in_map`, `not_analysed`.
- `warnings` — free-text warnings (e.g. a changed-but-unbuilt source edited after the map, from
  `changes`).
- `hidden_by_module` — with `--module` only: `{screens, components, affected}`, the screens,
  components (since 4.2.0) and affected symbols (seeds aside) outside the modules; `modules` and `packages` count only the ones shown.

```json
{
  "schema_version": "4.4.0",
  "command": "impact",
  "changed": [
    {
      "symbol": "navapp.impact.CounterActivity.getCount",
      "kind": "method", "module": "app",
      "location": "app/src/main/java/navapp/impact/Constructors.kt:9"
    }
  ],
  "affected": [
    {
      "symbol": "navapp.impact.CounterActivity", "kind": "class", "module": "app",
      "location": "app/src/main/java/navapp/impact/Constructors.kt:8",
      "depth": 1,
      "path": ["navapp.impact.CounterActivity.getCount", "navapp.impact.CounterActivity"]
    }
  ],
  "screens": [
    {
      "kind": "activity", "name": "FirstActivity", "target": "navapp.impact.FirstActivity",
      "module": "app", "location": "app/src/main/java/navapp/impact/Constructors.kt:12",
      "host": null, "hosts": [], "depth": 2, "opened_from": [],
      "path": ["navapp.impact.CounterActivity.getCount", "navapp.impact.CounterActivity", "navapp.impact.FirstActivity"]
    }
  ],
  "hidden": {}, "modules": { "app": 4 }, "packages": { "navapp.impact": 4 },
  "not_covered": [], "warnings": []
}
```

### impact --to

Result fields: `to` (the target argument, as given), `changed` (sorted qualified names of the
seeds), `steps` — the dependency path from a changed seed to the target, each step
`{symbol, kind, module, location, edge}` (`edge` is the edge-kind label into this step from the
previous one, `null` for the first step). Empty `steps` with no error means no path was found.

## changes

```
codemap changes [--base REF] [--head REF] [--diff -|FILE] [--files -|FILE] [--nav] [--depth N] [--kind KIND] [--no-paths] [--module GLOB]... [--json]
```

- `--base REF` — the branch the change forked from: the diff starts at the merge-base of `REF` and
  the head, so commits that landed on `REF` since do not count (default: the first of `changes.base`
  from config that resolves); with `--diff`/`--files`, the revision old values are read from instead
  of the changed diff lines, as given.
- `--head REF` — compare against this commit instead of the working tree; the code map **must**
  have been built from exactly this commit, clean (no uncommitted changes), or the command fails.
- `--diff -|FILE` — read a unified diff from stdin (`-`) or a file instead of running `git diff`.
- `--files -|FILE` — read a list of `path`, `path:N` or `path:N-M` lines instead of a diff.
- `--depth`, `--kind`, `--nav`, `--no-paths`, `--module`, `--json` — as in `impact`.
- `--base-db FILE` — the code map of the base (`codemap build --out FILE` on a checkout of it):
  what the change adds and removes, by the two maps (docs/decisions/0022). Without it
  `changed[].status`, `screens[].added` and `removed` are `null`.

`--diff` and `--files` are mutually exclusive. Without either, `changes` runs `git diff` itself
between the resolved base and `--head` (or the working tree).

A `--diff` whose context and added lines differ from the files the map was built from (the working
tree, or `--head`) is of another revision, and its line numbers may name other symbols: `warnings`
says so and names the files; with `--strict` it is an error, exit 3, with no answer.

Result: the shape of `impact` (it calls the same `reportImpactWith`), plus:

- `changed[].status` — with `--base-db`, `"added"` when the base's map has no node of the symbol's
  id and, for a member, its class there no member of its name (a function whose parameters changed
  stays `"modified"`; a lambda or anonymous class goes with the declaration it is in), else
  `"modified"`; `null` without `--base-db`. docs/decisions/0022.
- `screens[].added` — with `--base-db`, `true` when the base's map has no node of the screen's id (a
  route too), else `false`; `null` without. Whether a screen changed itself or through what it uses
  is its `depth`: 0 or more. Since 4.0.0, in place of `status`.
- `screens[].moved_from` — with `--base-db`, for a top-level Kotlin function (a preview) moved to
  another file of its package, the screen it was in the base's map: `{name, location}`, the
  location in the base; such a screen is not `added` and not in `removed`. Only when it is the one
  function of its package, name and parameters the other map lacks on each side.
  `null` otherwise and without `--base-db`. docs/decisions/0025. Since 4.1.0.
- `changed[].location` — for a resource, the changed file defining it, at the resource's line in a
  `values*/` file: `values-night/colors.xml:12`, not the default `values/`; an unqualified file
  first when several changed. Other symbols: their own location.
- `changed[].qualifiers` — for a resource, the qualifiers of the directories of its changed files,
  sorted (`["night"]` for `values-night/`); `[]` when an unqualified file changed (every
  configuration); `null` for code.
- `removed` — with `--base-db`, every screen of the base's map that is not a screen in the head's
  and did not move (`screens[].moved_from`), wherever its source (routes and nested classes included), `{kind, name, target, module,
  location}` with the location in the base, sorted by kind, module, name; `null` without.
- `screens[].qualifiers` — the qualifiers whose changes alone reach the screen (`["night"]`: only the
  night render changes); `null` when code or an unqualified resource reaches it.

and `meta.refs: {input, base, head, base_map}` where `input` is `"git"`, `"diff"` or `"files"`, `base` is the
resolved base ref/commit (or `null` without `--diff`/`--files` and no `--base`), `head` is the
resolved head commit or `"WORKTREE"`, and `base_map` is `{commit, variant, dirty}` of the `--base-db` map
(`null` without it). A base map of another commit than the resolved base, built from
a dirty tree or of another variant than the head's adds and removes what the change does not:
`warnings` says so, and with `--strict` it is an error, exit 3, with no answer.

```json
{
  "schema_version": "4.4.0",
  "command": "changes",
  "changed": [
    { "symbol": "@navigation/views_graph", "kind": "resource", "module": "app",
      "location": "app/src/main/res/navigation/views_graph.xml", "status": null,
      "qualifiers": [] },
    { "symbol": "navapp.impact.Session", "kind": "class", "module": "app",
      "location": "app/src/main/java/navapp/impact/Overrides.kt:26", "status": null,
      "qualifiers": null }
  ],
  "affected": ["... same shape as impact ..."],
  "screens": ["..."],
  "removed": null,
  "hidden": {}, "modules": { "app": 26 }, "packages": { "navapp.impact": 23, "navapp.views": 1 },
  "not_covered": [], "warnings": []
}
```

## diff

```
codemap diff --base FILE [--module GLOB]... [--json]
```

What the map (`--db`/`--variant`, as any query) has and the map at `--base` has not, and the other
way round, by node id (docs/decisions/0023): another variant's map (`--base
build/codemap/release.snap`: a debug-only screen, a release-only path) or another revision's (`build
--out` on a checkout of the base). Since 4.0.0.

- `--base FILE` (required) — the other map.
- `--module GLOB` — only screens in matching modules; an open when either end is in one.

Result: `screens`, `entries` and `opens`, each `{added, removed}`: `added` what only the map has,
`removed` what only `--base` has, each named and located as in the map that has it.
- `screens` — `{name, kind, module, location, host, hosts}` as in `nav-path` (previews and layouts
  too), sorted by kind, module, name.
- `entries` — the manifest's ways into a screen and its `push` entries, `{kind, detail, screen,
  module}`, sorted by kind, detail, screen; the entries of an added or removed screen are listed too.
- `opens` — a screen opening another, as `graph` draws it: `{from, to, from_module, to_module}`,
  sorted by from, to; every open of an added or removed screen is listed (the text shows only
  those between screens both maps have, and counts the rest).
- `warnings` — maps built with other settings than the variant (another `codemap.yml`) differ with
  no change of code: said here, exit 3 with `--strict`.

`meta.refs: {base_map: {commit, variant, dirty}}` names the map at `--base`.

```json
{
  "schema_version": "4.4.0",
  "command": "diff",
  "screens": {
    "added": [{ "name": "LinkActivity", "kind": "activity", "module": "app",
      "location": "app/src/main/java/navapp/views/ViewsActivity.kt:59", "host": null, "hosts": [] }],
    "removed": []
  },
  "entries": {
    "added": [{ "kind": "deeplink", "detail": "https://navapp.example/views", "screen": "LinkActivity", "module": "app" }],
    "removed": []
  },
  "opens": {
    "added": [{ "from": "LinkActivity", "to": "ViewsActivity", "from_module": "app", "to_module": "app" }],
    "removed": []
  },
  "warnings": []
}
```

## crash

```
codemap crash [-|FILE] [--mapping FILE] [--nav] [--depth N] [--kind KIND]... [--no-preview] [--no-paths] [--module GLOB]... [--json]
```

Where a crash's stack trace ran in the map, and the screens and components it ran for
(docs/decisions/0029). The trace comes from FILE or stdin (`-`, the default) as Crashlytics
exports it, logcat prints it or Java writes it; only the crashing thread is read (`Caused by`
included, `Suppressed` not). Since 4.3.0.

- `--mapping FILE` — the R8/ProGuard `mapping.txt` of the build that crashed: frames it renames
  are read by their source names and lines; an inlined frame becomes the chain it stands for.
- `--nav`, `--depth`, `--kind`, `--no-preview`, `--module` — as in `impact`, on the screens.

Result:
- `exceptions` — `{class, message}` of the exception and its causes, outermost first.
- `frames` — every frame of the chain, deepest first within an exception: `{exception, class,
  method, file, line, obfuscated, status, symbol, module, location, reach}`. `exception` indexes
  `exceptions`; `obfuscated` is the frame as the trace had it (`{class, method, file, line}`)
  when `--mapping` renamed it, else `null`; `status` is `resolved` (its method, or its class, is in
  the map), `not_found` (its package is in the map, its class is not: removed or made by R8),
  `generated` (DI code the map leaves out) or `library`. For a resolved frame `symbol`, `module`,
  `location` (the trace's line in the method's file) and `reach` (the screens and components a
  walk from it reaches, as `impact` walks); `null` otherwise. `line` is `null` when the trace has none.
- `crashed_at` — the deepest resolved frame of the innermost cause that has one; `entry` — the
  outermost resolved frame of the outermost exception that has one; `screens_of` — the resolved
  frame with the smallest non-zero `reach`, the nearest the crash on a tie. Each `{frame, symbol,
  module, location, in_kind, in}`, `frame` indexing `frames`, `in_kind`/`in` the component
  (`application`, `service`, `receiver`, `provider`, `worker`) or screen class (`activity`,
  `fragment`, `dialog`) the frame's code is in; `null` when there is no such frame.
- `screens`, `components`, `hidden` — as in `impact`, from the walk of `screens_of`; empty
  without it.
- `warnings` — no frame in the map (obfuscated without `--mapping`, a library's crash, another
  app's map), no own frame reaching a screen or component, a mapping that renames nothing.
- `hidden_by_module: {screens, components}` with `--module`.

```json
{
  "schema_version": "4.4.0",
  "command": "crash",
  "exceptions": [{ "class": "java.lang.IllegalArgumentException", "message": "negative amount: -1" }],
  "crashed_at": { "frame": 0, "symbol": "navapp.crash.Money.format", "module": "app",
    "location": "app/src/main/java/navapp/crash/Crash.kt:19", "in_kind": null, "in": null },
  "entry": { "frame": 1, "symbol": "navapp.crash.InvoiceViewModel.load.1.invokeSuspend", "...": "..." },
  "screens_of": { "frame": 1, "symbol": "navapp.crash.InvoiceViewModel.load.1.invokeSuspend", "...": "..." },
  "frames": [
    { "exception": 0, "class": "navapp.crash.Money", "method": "format", "file": "Crash.kt", "line": 19,
      "obfuscated": { "class": "jg0", "method": "a", "file": "SourceFile", "line": 40 },
      "status": "resolved", "symbol": "navapp.crash.Money.format", "module": "app",
      "location": "app/src/main/java/navapp/crash/Crash.kt:19", "reach": 3 },
    { "exception": 0, "class": "kj", "method": "e", "file": "SourceFile", "line": 9, "obfuscated": null,
      "status": "library", "symbol": null, "module": null, "location": null, "reach": null }
  ],
  "screens": [{ "kind": "fragment", "name": "InvoiceFragment", "...": "... as in impact ...",
    "path": ["navapp.crash.InvoiceViewModel.load.1.invokeSuspend", "navapp.crash.InvoiceViewModel.load",
      "navapp.crash.InvoiceFragment.onViewCreated"] }],
  "components": [],
  "hidden": {},
  "warnings": []
}
```

## nav-path

```
codemap nav-path SCREEN [--via SCREEN]... [--ways] [--from SCREEN | --entry launcher|deeplink|intent|push]
                 [--avoid GLOB]... [--verbose] [--limit N] [--depth N] [--no-paths] [--all] [--json]
```

- `SCREEN` (positional, required) — a screen name/target, or (falling back) a symbol whose nearest
  enclosing screen(s) are used as the target(s).
- `--verbose` — include every way found (`hubs`, `bases`), not just the cheapest ones on the
  reported paths/branches.
- `--limit N` — cap how many branch ways are reported (text output; default 20).
- `--via SCREEN` (repeatable, in order) — the cheapest way through these screens: the cheapest part
  from the targets back to the last of them, from there to the one before, and from the first to an
  entry. The result has the regular keys for that one way plus `via` (the arguments); no way gives
  empty `screens`, `entries`, `paths`.
- `--ways` — the cheapest way through each screen that opens the *fork*: the first screen back from
  the target, on its cheapest way, that more than one screen opens (FaqActivity for FaqFragment). The
  last step into the fork is the screen's own. Not with `--via`. Result: `targets`, `fork` (name or
  `null` when no screen on the way has two openers: then `ways` holds the cheapest way), `ways`
  (`{through, cost, entry, path}`, `entry` an `entries` item; sorted as the entries: by entry kind,
  then cost, then `through`), `no_way` (openers of the fork no entry reaches).
- `--from SCREEN` — ways start at this screen in whatever host instead of at an entry: its `entry`
  is `[{"kind": "from", "detail": null}]`; no `base`/`outside`/`unknown` entries, no `branches`.
- `--entry KIND` — ways start only at entries of this kind (`launcher`, `deeplink`, `intent`, `push`); no
  `base`/`outside`/`unknown` entries, no `branches`. Not with `--from`.
- `--avoid GLOB` (repeatable) — no way goes through a screen whose name or class matches (`*`: any
  run of characters), e.g. `--avoid 'Debug*'`.
- `--depth N`, `--all`, `--json` — as above.

Result fields:
- `targets` — the resolved target screen(s), `{name, kind, module, location, host, hosts}`.
- `screens` — every screen on a reported way (the cheapest paths, plus the kept branch ways; all of
  them with `--verbose`), each `{name, kind, module, location, host, hosts, depth, cost, hosted_in,
  opens, way, shared, hint, via}`. `cost` is the walk cost in `nav_path.costs` units; `way` is
  `"own"`/`"shared"`, prefixed `"push "` when the step goes through push/deep-link handling;
  `shared` is the qualified name of the shared "hub" code when `way` is shared, else `null`;
  `hint` is the step's human label (`stepHint`); `via` is the qualified code path of the step.
- `entries` — how the target is first reached: screen object plus `entry`, an array of
  `{kind, detail}` (`kind` one of `launcher`, `deeplink`, `intent`, `push`, `base`, `outside`,
  `unknown`, `from` with `--from`; `detail` extra text or `null`). `push` (since 4.2.0): the code
  of a service or receiver whose intent filter takes a push (`nav_path.push_actions`) opens the
  screen without a screen on the way; `detail` is the component's name (docs/decisions/0028).
- `paths` — array of arrays of screen names, one per entry point, from the entry to the target.
- `branches` — other launcher branches within `nav_path.branch_slack`: `{entry, through, cost,
  shared, path}`.
- With `--verbose` only: `hubs` — `{opens, via, from}` (shared code and what screens reach it
  through it); `bases` — `{opens, base, subclasses, screens}` (a common base-class way: how many
  subclasses, how many screens behind it).

```json
{
  "schema_version": "4.4.0", "command": "nav-path",
  "targets": [{ "name": "Login", "kind": "route", "module": "app",
    "location": "app/src/main/java/navapp/typed/TypedApp.kt", "host": null, "hosts": [] }],
  "screens": [
    { "name": "Home", "kind": "route", "module": "app",
      "location": "app/src/main/java/navapp/typed/TypedApp.kt",
      "host": null, "hosts": [], "depth": 1, "cost": 1, "hosted_in": null,
      "opens": "Login", "way": "own", "shared": null,
      "hint": "TypedAppKt.TypedApp",
      "via": ["navapp.typed.TypedAppKt.TypedApp$lambda$0$0$0$1$0", "route Home"] }
  ],
  "entries": [{ "name": "MainActivity", "kind": "activity", "module": "app",
    "location": "app/src/main/java/navapp/MainActivity.kt:13", "host": null, "hosts": [],
    "entry": [{ "kind": "launcher", "detail": null }] }],
  "paths": [["MainActivity", "Home", "Login"]],
  "branches": [{ "entry": "MainActivity", "through": "Home", "cost": 2, "shared": null,
    "path": ["MainActivity", "Home", "Login"] }]
}
```

## graph

```
codemap graph SCREEN [--to SCREEN | --in] [--avoid GLOB]... [--module GLOB]... [--depth N]
              [--direction LR|RL|TB|TD|BT] [--all] [--json]
```

- `SCREEN` (positional, required) — same resolution as `nav-path`'s `SCREEN`; can match more than
  one screen (multiple roots).
- `--depth N` — how many hops out from the root(s) to draw (default 1).
- `--to SCREEN` — draw only the ways that reach this screen within `--depth` (default: the fewest
  steps from the root plus 2, so a detour or two shows); a way back into a root is a loop and is left
  out. When none gets there in time, the text ends with a `%% no way to ...` comment and `edges` is
  empty. Not with `--in`.
- `--in` — the other direction: what opens the root(s), and what opens those, up to `--depth`.
  Shared code stands between its callers and the screen; with more than `graph.fanout` callers they
  go into `more`. Hosts are not followed: every way that opens a screen is drawn.
- `--avoid GLOB` (repeatable) — leave out screens whose name or class matches (`*`: any run of
  characters), and what is reached only through them.
- `--module GLOB` (repeatable) — walk on only from screens in a matching module (see Module globs;
  unlike the other commands it limits the walk); a screen elsewhere is drawn but not walked from (a
  feature's map: `--module feature/cards`).
- `--direction` — how the Mermaid flowchart runs (text output only): `LR` (default), `RL`, `TB`
  (or `TD`), `BT`.
- `--all`, `--json`.

Result fields:
- `roots` — the root screen name(s).
- `walk` — `"out"` (what the roots open) or `"in"` (what opens them, `--in`).
- `to` — the `--to` screen name(s), `[]` without it.
- `nodes` — `{name, kind, module}`; `kind` is a screen kind, `"action"` (one method opening several
  screens, drawn as its own node), or `"shared"` (hub code).
- `edges` — `{from, to, label, hint, shared}`; `label` is the short action name shown on the edge
  (`null` if none), `hint` the full step hint, `shared` the target node's name again when the edge
  goes through shared/hub code (else `null`).
- `more` — object keyed by hub node name, each value the (alphabetized) list of screen names folded
  into that hub's `+N` node because they exceeded `graph.fanout`.
- `edges_left_out` — edges dropped once `graph.max_edges` was exceeded.
- `ways_of_other_hosts` — ways not drawn because they need a host (Activity/module) not reachable
  from the root in this context.
- `debug_left_out` — debug-only screens hidden because the root itself is not debug-only.

```json
{
  "schema_version": "4.4.0", "command": "graph",
  "roots": ["Home"], "walk": "out", "to": [],
  "nodes": [
    { "name": "Home", "kind": "route", "module": "app" },
    { "name": "Home: TypedAppKt.TypedApp", "kind": "action", "module": "app" },
    { "name": "Login", "kind": "route", "module": "app" },
    { "name": "Profile", "kind": "route", "module": "app" }
  ],
  "edges": [
    { "from": "Home", "to": "Home: TypedAppKt.TypedApp", "label": null,
      "hint": "TypedAppKt.TypedApp", "shared": null },
    { "from": "Home: TypedAppKt.TypedApp", "to": "Login", "label": null,
      "hint": "TypedAppKt.TypedApp", "shared": null }
  ],
  "more": {}, "edges_left_out": 0, "ways_of_other_hosts": 0, "debug_left_out": 0
}
```

## screens

```
codemap screens [FILTER] [--kind KIND] [--no-preview] [--module GLOB]... [--host SCREEN] [--unreachable] [--json]
```

- `FILTER` (optional positional) — case-insensitive substring match against name/target/module/host
  and the composables a route draws (`content`): `screens CreateConfirmScreen` finds its route.
- `--module GLOB` — only screens in a matching module (see Module globs).
- `--host SCREEN` — only screens whose `hosts` hold this screen (an Activity hosting fragments and
  dialogs, the screen hosting routes); the direct host, not the host of the host.
- `--unreachable` — only screens (activity, fragment, dialog, route) that no entry of the manifest
  reaches: the `nav-path` walk back from the screen ends only where nothing opens a screen
  (`unknown`) or where only code outside the screens opens it (`outside`). A way ending at a base
  class (`base`: every screen extending it opens the screen, such as a popup `BaseActivity` shows)
  counts as reached. A dead-screen audit, as far as the code map sees.
- `--no-preview` — only activities, fragments, dialogs and routes no preview draws (`previews`
  empty): screens no preview shows. Also in `impact` and `changes`.
- `--kind KIND`, `--json`.

Result fields: `screens` — every matching screen, `{kind, module, name, target, host, hosts,
location, content, previews}`, sorted by kind, module, name, target, host, location; `content` — for a route,
the repo composables its registration draws, by label (`CreateConfirmScreenKt.CreateConfirmScreen()`:
those called from the code written at the registration, not from an inlined wrapper such as
`screen<VM> {}`), sorted; for a preview, the repo composables it draws, through its lambdas (since 3.7.0); a
composable returning a value (a theme getter, `remember*`) is not drawn; empty for
other kinds and for a route drawing only library composables; `previews` — the targets of the
previews drawing the screen's own composables, sorted (since 3.7.0): a preview goes with the screens
drawing a composable of its own file (else module) that the screen draws itself or through a
composable of its own module calling it, those the fewest screens draw when there are several, of
those the screens drawing a composable of the preview's file (or an Activity, fragment of it) when there are any; what a route draws is
not its host Activity's or fragment's (docs/decisions/0026);
so a theme or a design system's component links no screen; empty for
previews and layouts; `hidden` — counts of screens
excluded by `--kind`, by kind (of those in the modules); `hidden_by_module` — with `--module` only:
`{screens}`, the screens outside the modules. With `--unreachable` each screen also has `entries`:
where the walk back ends, as in `nav-path` (`{name, kind, module, location, host, hosts, entry}`,
`entry` an array of `{kind, detail}`).

```json
{
  "schema_version": "4.4.0", "command": "screens",
  "screens": [
    { "kind": "activity", "module": "app", "name": "AvatarActivity",
      "target": "navapp.impact.AvatarActivity", "host": null, "hosts": [],
      "location": "app/src/main/java/navapp/impact/DataClasses.kt:18", "content": [], "previews": [] },
    { "kind": "dialog", "module": "app", "name": "InfoDialog",
      "target": "navapp.views.InfoDialog", "host": "ViewsActivity", "hosts": ["ViewsActivity"],
      "location": "app/src/main/java/navapp/views/ViewsActivity.kt:48", "content": [], "previews": [] },
    { "kind": "route", "module": "app", "name": "Home", "target": "navapp.typed.Home",
      "host": "MainActivity", "hosts": ["MainActivity"],
      "location": "app/src/main/java/navapp/typed/TypedApp.kt", "content": ["TypedAppKt.HomeScreen()"],
      "previews": [] }
  ],
  "hidden": {}
}
```

## find

```
codemap find PATTERN [--all] [--limit N] [--type TYPE]... [--module GLOB]... [--json]
```

- `PATTERN` (positional, required) — a regexp (falls back to a literal substring if invalid)
  matched against the qualified name or simple name; additionally must match as a
  SQLite-`LIKE`-style ordered-segment match against the symbol's simple/qualified name (letters
  split on non-word characters).
- `--all` — also include synthetic/generated symbols (Hilt/Dagger glue, lambdas, `$DefaultImpls`)
  normally hidden.
- `--limit N` — cap the number of symbols shown (default: unlimited; `--limit` only affects the
  text/JSON `symbols` list, not `total`).
- `--type TYPE` (repeatable) — only symbols of this `kind`: `class`, `method`, `field`, `resource`.
- `--module GLOB` — only symbols in a matching module (see Module globs).

Result fields: `pattern` (as given), `total` (count of all matches before `--limit`), `synthetic`
(count of matches hidden because they are synthetic, when `--all` is not given), `symbols` — array
of `{symbol, kind, module, location}`, sorted by qualified-name length then kind then name then
module then location, truncated to `--limit`; `hidden_by_module` — with `--module` only:
`{symbols}`, the matches outside the modules (not in `total`).

```json
{
  "schema_version": "4.4.0", "command": "find", "pattern": "Activity",
  "total": 15, "synthetic": 0,
  "symbols": [
    { "symbol": "navapp.MainActivity", "kind": "class", "module": "app",
      "location": "app/src/main/java/navapp/MainActivity.kt:13" },
    { "symbol": "@layout/activity_views", "kind": "resource", "module": "app",
      "location": "app/src/main/res/layout/activity_views.xml" }
  ]
}
```

## build --info

```
codemap build --info [--json]
```

No positional arguments. Reports the metadata of the current snapshot without touching it.

Result fields: `index` — every key/value of the snapshot's meta table, sorted by key:
`build_s`, `built_at`, `class_dirs`, `class_files`, `config` (the settings the map was built with,
as a JSON string), `dirty`, `edges`, `git_head`, `index_version`, `inputs_sha1`, `module_paths`,
`nodes`, `parse_errors`, `parse_s`, `resources`, `root`, `screens`, `source_files`, `tool_sha1`;
`screens` — counts of screens by kind; `components` — counts of components by kind (since 4.2.0).

## Versioning policy

`schema_version` is semver, set by hand in `internal/query/cli.go` (`schemaVersion`):

- **Major**: a field is removed or renamed, its type changes, or its meaning changes in a way that
  breaks a consumer of today's shape.
- **Minor**: a field is added to an object, a command is added, or a field gets a new value (a new
  screen kind, a new entry kind).
- **Patch**: a fix that keeps the shape, such as a field that held a wrong value.

Formatting (indentation, key spacing) is not part of the contract; parse the JSON.

