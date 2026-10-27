# codemap

[![CI](https://github.com/v-kravchenko/codemap/actions/workflows/ci.yml/badge.svg)](https://github.com/v-kravchenko/codemap/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/v-kravchenko/codemap)](https://github.com/v-kravchenko/codemap/releases/latest)
[![Coverage](https://codecov.io/gh/v-kravchenko/codemap/graph/badge.svg)](https://codecov.io/gh/v-kravchenko/codemap)
[![Go](https://img.shields.io/github/go-mod/go-version/v-kravchenko/codemap)](go.mod)
[![License](https://img.shields.io/github/license/v-kravchenko/codemap)](LICENSE)

The code map of an Android project, built from its compiled classes and resources: what a change
affects (`impact`, `changes`), who uses a symbol (`used-by`, `uses`), how a user reaches a screen
(`nav-path`), what a screen opens (`graph`). Every command has a stable `--json`.

`build` writes a binary snapshot of the map (what the CLI loads, mapped: ~50 ms a query) and walks
who opens every screen. A full build of a large app (~35k classes) takes ~3 s.

```
$ codemap impact CartViewModel.total
Changed:
  CartViewModel.total()  app  app/src/main/java/navapp/impact/GenericDi.kt:22

Screens: 1  (fragment 1)

Fragments
  CartFragment                                       app
      CartViewModel.total() -> CartFragment.onViewCreated()

$ codemap nav-path InfoDialog
Cheapest way to InfoDialog (launcher):
  MainActivity              MainActivity.onCreate
  -> ViewsActivity          @navigation/views_graph
  -> ListFragment           ListFragment.onViewCreated
  -> DetailFragment         DetailFragment.onViewCreated
  -> InfoDialog
```

## Install

On Linux or macOS, in CI too (`CODEMAP_VERSION=0.3.0` pins a release, `CODEMAP_INSTALL_DIR` changes
the directory, default `~/.local/bin`; the download is checked against the release's checksums):

```
curl -fsSL https://raw.githubusercontent.com/v-kravchenko/codemap/main/install.sh | sh
```

A binary for Linux, macOS or Windows from [Releases](https://github.com/v-kravchenko/codemap/releases/latest)
(`gh attestation verify codemap_*.tar.gz -R v-kravchenko/codemap` checks it was built here), or
with Go:

```
go install github.com/v-kravchenko/codemap/cmd/codemap@latest
```

### In a project: codemapw and the build hook

`codemap wrapper` writes four files to commit with the project: `codemapw` and `codemapw.cmd`,
which download this release once (to `$CODEMAP_HOME`, default `~/.cache/codemap` or
`%LOCALAPPDATA%\codemap`) and check it against the sha256 in `codemap-wrapper.properties`, and
`gradle/codemap.gradle`. Applied at the end of `settings.gradle` (`apply from:
'gradle/codemap.gradle'`; in `.kts`, `apply(from = "gradle/codemap.gradle")`), it makes each
build of the project keep the map fresh: the configuration writes the model (no `codemap init`),
and a build that compiles a variant, even up to date, runs `codemapw build --variant` for it
before it ends (a failure is a warning, `CODEMAP_SKIP=1` skips it). After a pull, a build is all
a developer needs before `./codemapw screens`. `--version` picks another release, `--distribution-url` a mirror;
keep `build/codemap` and `codemap.model.json` out of git.

## Usage

```
codemap init [--gradle CMD]                        # once, and after build files change
codemap wrapper [--version V]                      # codemapw and the build hook for the project
codemap build [--variant V] [--out SNAPSHOT] [--jobs N] [--force]  # default: <variant>.snap in snapshot_dir
codemap used-by|uses|impact|changes|nav-path|graph|screens|find|build --info ... [--variant V] [--json]
codemap COMMAND --help                             # its options; one it does not take is an error
codemap version                                    # the release, or dev for a build from a checkout
CODEMAP_TIMES=1 codemap build                      # time of each stage on stderr
CODEMAP_PROFILE=/tmp/cpu.prof codemap impact X     # CPU profile of a build or a query
```

`nav-path` shows the cheapest way to a screen; `--via SCREEN` a way through given screens, `--ways`
one way through each screen that opens it, `--from SCREEN` a way from a screen the user is on,
`--entry deeplink` a way from one kind of entry, `--avoid 'Debug*'` a way around some screens.

`graph` draws what a screen opens as a Mermaid flowchart; `--to SCREEN` only the ways to a screen,
`--in` what opens it, `--module feature/cards` one feature's map, `--avoid GLOB` without some screens;
`--direction TB` draws it top to bottom (`LR`, `RL`, `TB`/`TD`, `BT`).

`--module GLOB` keeps the answer of `impact`, `changes`, `used-by`, `uses`, `screens` and `find` to
some modules, as `.gitignore` takes directories: `feature/cards` with the modules under it,
`'!feature/debug'` everything but, `**/impl` every `impl` (docs/json.md, Module globs).
`screens --unreachable` lists the screens no entry of the manifest reaches, and where the way back
ends; `screens --host X` the screens an Activity hosts; `screens CreateConfirmScreen` the route that
draws a composable; `find --type class` one kind of symbol;
`used-by --no-di` without generated DI code.

`build --variant release` builds the map of another variant next to the configured one
(`build/codemap/release.snap`); a query reads it with `--variant release`.
`build` skips a map built by the same binary from the same inputs (`--force` rebuilds it). Queries
check whether the map is older than the sources (`--strict`: exit with an error when the answer
touches a changed file). `changes` needs git; nothing else does (`build` records HEAD when there is one).

## Project model: codemap init

`codemap init [--gradle CMD]` runs Gradle once (`help` with `codemap.gradle`, configuration
only, nothing compiled; `--gradle gw` or `$CODEMAP_GRADLE` for another launcher) and writes
`codemap.model.json`: the modules with their Gradle paths, and for every variant the class
directories (the compile tasks' outputs), sources, resources and manifests of its source sets. With
the model `build` takes the modules of `variant` from it, names the modules not compiled with the
tasks that compile them, and stops when the build files (settings, build scripts, version catalogs,
gradle.properties, included builds) changed since `init`. Without it the modules are found by
walking the tree for `build.class_dirs`. The build hook (`codemap wrapper`) writes the model on
each configuration instead, and `build` takes it over when it is newer than the build files.

## Settings: codemap.yml

`codemap.yml` at the root (next to `settings.gradle`) holds what is particular to the project: the
variant, the class directories, the screen base classes, the navigation calls, the nav-path costs.
`extends` merges built-in presets first (`internal/config/presets/*.yml`); lists add up
from the presets to the file, except `build.class_dirs`, `build.source_sets` and `changes.base`,
which replace; a scalar set later wins. Unknown keys are errors. Without the file the tool runs on
`extends: [android, nav2-compose]`. The map keeps the settings it was built with: `build` rebuilds
when they change, the queries read them from the map.

```
codemap config check    # the file and its presets load, patterns compile
codemap config print    # the settings after the presets
codemap config schema   # codemap.schema.json (editors: yaml-language-server)
```

Class names are dotted (`com.example.Base`, `Outer$Inner`); `navigation.register`,
`navigation.open` and `navigation.graph` are `Class.member` globs: `*` is a run of word characters,
the last dot parts the class from the member, `name$default` matches too.

Presets:

| Preset | For |
|---|---|
| `android` | Activity, Fragment and DialogFragment bases; class dirs of AGP 8 and 9 |
| `dagger-android` | the screen bases of dagger-android |
| `nav-events` | a route handed over a Flow or LiveData opens it (the others extend it) |
| `nav2-compose` | Navigation Compose, type-safe: `composable<T>`, `navigation<T>(startDestination = S)`, `navigate(Route)` |
| `nav2-compose-strings` | Navigation Compose, string routes: `composable("post/{id}")`, `navigate("post/$id")` |
| `nav3` | Navigation 3: `entry<T>`, `is T -> NavEntry(key)`, `backStack.add(key)`, a call taking a `NavKey` |

`navigation.typed_routes` (nav2-compose, nav3): a route is the class of the class literal (or the
`is` check) before a `register` call; an object or a constructor call handed to an `open` call, or
left in a method, opens it. The route's own code, its sealed parent's and the deep links built from
a URI at run time do not count. `navigation.string_routes`: a route is the string constant before a
`register` call; `{arg}` and the parts of a string template match any path segment, `?query` is
left out; a route kept in a variable or passed as `Any` is not seen. `navigation.graph`: a nested
graph from its start route and its own route; opening the graph opens the start route.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md): the parts, the data flow, the rules of the walks, invariants
- [docs/config.md](docs/config.md): every setting of `codemap.yml` and the presets
- [docs/json.md](docs/json.md): the `--json` contract of each command, exit codes
- [docs/limitations.md](docs/limitations.md): what the map does not see or gets wrong
- [docs/decisions](docs/decisions): why it is built this way (ADRs)
- [ROADMAP.md](ROADMAP.md): what is planned
- [AGENTS.md](AGENTS.md): how to work on the code
- [CHANGELOG.md](CHANGELOG.md): what changed in each release
- [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md): issues, pull requests, reporting a vulnerability

## Checks

```
go vet ./... && go test ./...
```

`internal/build/navigation_test.go` builds the map of `testdata/navapp`, an app with one screen set per navigation
preset and one of fragments in an XML navigation graph (an `<action>`, a nested graph, a dialog, an
Activity started with an Intent); `testdata/navapp/update.sh` compiles it into `classes.zip` (Android SDK, Gradle).

`cmd/codemap/golden_test.go` runs the CLI on the fixture and compares the `--json` of each call in `goldenCalls`
(without `meta`) with `testdata/golden`. The impact calls are the precision rules of the walk, one
scenario each in `testdata/navapp/app/src/main/java/navapp/impact`. A change that means to change
an answer rewrites the files with `go test -run TestGolden -update`; the diff is part of the review.
