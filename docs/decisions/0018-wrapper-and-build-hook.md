# 0018. A wrapper and a build hook: the checkout runs codemap, the build keeps the map fresh

- Status: accepted
- Date: 2026-10-27

## Context

In a team, codemap is one more tool each developer installs, keeps at the team's version, and
remembers to rerun: `codemap init` after a build file changes, `codemap build` after compiling.
A map older than the classes answers about yesterday's code. The aim: after pulling the project,
a developer builds it as always and `./codemapw` answers about what was just compiled, on Linux,
macOS and Windows, in CI too, with nothing in codemap that knows the project.

0005 turned down a Gradle task in every build: codemap would depend on a working Gradle and on
build-logic in each project, and every build would pay for it. That still holds for codemap
itself; what changes is that a project may now opt in.

## Decision

`codemap wrapper [--version V]` writes four files a project commits, as Gradle's wrapper does
(for another release V, V writes them: codemap runs it by codemapw from a temporary directory):

- `codemapw` (sh) and `codemapw.cmd` (PowerShell inside): download the release once into
  `$CODEMAP_HOME` (`~/.cache/codemap`, `%LOCALAPPDATA%\codemap`), check its sha256, run it.
- `codemap-wrapper.properties`: the version, `distribution_url` (a mirror replaces GitHub) and
  the sha256 of each archive, read from the release's `checksums.txt` when the wrapper is written.
  This and `init` are the commands that reach out; `build` and the queries still never do.
- `gradle/codemap.gradle`, applied at the end of `settings.gradle(.kts)`: the script `codemap
  init` runs as an init script, which in the settings also
  - writes the model on each configuration to `build/codemap/gradle-model.json`; `build` takes
    it over as `codemap.model.json` when it is newer than every build file, with the hash of the
    build files computed in Go, so the format and the stale check stay codemap's;
  - after a build in which a compile task of a variant ran, up to date or not (the map may be
    missing; `build` skips itself in ~0.15 s when it is fresh), runs `codemapw
    build --variant` for each such variant, synchronously: when the build ends, the map is the
    classes'. A failure is a warning; `CODEMAP_SKIP=1` turns it off (a CI job that does not
    need the map).

The hook is a build service listening to task completion (`BuildEventsListenerRegistry`), the
way that works with the configuration cache: on a cache hit the configuration, and the model
with it, is skipped, which is right, as the build files did not change. The model is written,
never read, by the configuration: a file it read would become an input of the cache.

### Timeouts and the failed-download marker (0.10.1)

A build must not wait on a download. The defaults do: curl's connect timeout is 300 s, wget's
read timeout 900 s, PowerShell 5.1 has none (`TimeoutSec` 0 is unlimited). So each step has a cap:

- curl: `--connect-timeout 10 --max-time 120 --speed-limit 1024 --speed-time 30` (a transfer
  under 1 KiB/s for 30 s is cut); wget: `--timeout=10 --tries=1`; PowerShell:
  `-TimeoutSec 120`.
- A failed download writes `$CODEMAP_HOME/<version>/.download-failed`; a successful one removes it.
  A call with `CODEMAP_HOOK=1` (the hook sets it) that finds the marker younger than 15 minutes
  does not try the download and fails at once; a manual `./codemapw` always tries. Fifteen minutes
  is not about the timeouts: while the network is down, a build pays at most one attempt per 15
  minutes instead of one per build.
- The hook waits for codemap 3 minutes at most, then stops it. That bounds what the download caps
  do not: wget has no cap on the whole transfer, and on PowerShell 5.1 `TimeoutSec` bounds the wait
  for the response. A stopped `build` leaves no partial map: the snapshot is written to a temp file
  and renamed (`Map.WriteSnapshot`).
- The hook's output goes to a temp file, not a pipe: a pipe read to its end would hold the build
  past the wait.

## Consequences

- A project opts in with the four files and one line in its settings; without them codemap works
  as before (`codemap init`, `codemap build`).
- A build that compiled pays the `codemap build` after it, ~3 s on a project of 80 modules (one
  with nothing compiled ~0.15 s for the check), and
  on a configuration (no cache hit) writing the model, under a second.
- Two builds of one project at once each write the map through a temp file of their own; the
  last one wins, whole.
- `allprojects` in the script does not go with Gradle's Isolated Projects (docs/limitations.md).
- A release's archives and checksums must stay where `distribution_url` says.

## Alternatives considered

- **A Gradle plugin on the Plugin Portal** (settings plugin, the Variant API, the binary as Maven
  artifacts per system, as protoc). The right final form for teams behind a Maven proxy and for
  Isolated Projects, but a Kotlin codebase, two more publishing accounts and a test matrix of
  AGP and Gradle versions to keep; the hook moves into it as it is when someone needs it.
- **Each project's build-logic does it.** Every project would copy the model logic, and the
  model's format would become an interface between codemap and code it does not own.
- **An init script in `~/.gradle/init.d`.** Nothing to commit, but each developer installs it,
  and it applies to every build on the machine.
- **The map rebuilt in the background.** The build ends sooner, but a query right after it reads
  the old map, and two builds start two rebuilds.
- **Checksums not pinned** (codemapw reads `checksums.txt` of the release when it downloads, as
  install.sh). Simpler, but the download is then checked only against the same server.
