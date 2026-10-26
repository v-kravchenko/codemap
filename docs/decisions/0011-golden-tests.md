# 0011. Golden tests on a compiled fixture

- Status: accepted
- Date: 2026-10-26

## Context

While codemap lived inside the Android app it was built for, its safety net during the rewrite was
differential: run the new (Go) implementation and the old (prototype) implementation against that
same real app's compiled output, and diff every table and every sampled CLI call between them. That
caught real regressions (ordering bugs, a dropped edge kind, a dropped source-set) throughout the
port, precisely because the two implementations' outputs had to agree byte-for-byte or
semantically, table by table.

That approach stops being available once the tool is extracted into its own repository: there is
no real Android app shipped alongside it to build, no prototype left to compare against, and no
guarantee that whoever changes the tool next has access to (or the rights to use) any particular
real Android codebase at all. A test suite built around one real, external project would also tie
every future precision fix to whether that one project's code still produces the same answer,
rather than to a documented, intentional rule.

## Decision

Tests are golden `--json` outputs of the CLI, run against a small Android app purpose-built as a
test fixture (`testdata/navapp`) and committed to this repository as compiled bytecode
(`classes.zip`), not against any real, external project.

- `testdata/navapp` is a minimal Android app with one set of screens per supported navigation
  preset (Navigation Compose type-safe routes, Navigation Compose string routes, Navigation 3, XML
  navigation graphs with Fragments — including a nested graph, an `<action>`, a dialog destination,
  and an Activity started via `Intent`), plus one scenario per precision rule the walk implements
  (an overridden method that cuts off a base class's fan-out, a default-argument call, a
  constructor that overrides nothing, a generic-base-class view model, a `const val` resolved
  through source text, generated `copy`/`equals` not spreading an edge, and so on) under
  `testdata/navapp/app/src/main/java/navapp/impact`.
- `testdata/navapp/update.sh` compiles the fixture's sources into `classes.zip` using a real
  Android SDK and Gradle; the tests read that committed zip, not the sources, so running the test
  suite itself needs no Android SDK.
- `cmd/codemap/golden_test.go` runs the actual CLI (`TestMain` re-executes the test binary as
  `codemap` when an environment variable is set) over a fixed list of calls (`goldenCalls`) and
  compares each call's `--json` output, with the non-deterministic `meta` block stripped, against a
  recorded file in `testdata/golden`. A change that is meant to change an answer re-records the
  golden files (`go test -run TestGolden -update`), and the resulting diff is what a reviewer reads
  to confirm the change is the intended one.

## Consequences

- The test suite has no dependency on any particular real Android project, its licensing, or its
  continued existence; anyone can clone this repository and run `go test ./...` with nothing beyond
  the Go toolchain.
- Coverage is explicit and enumerable: each precision rule in the walk has its own named scenario in
  the fixture, rather than coverage being an emergent, hard-to-audit property of whatever one real
  app happens to contain. A new precision rule is expected to come with a new fixture scenario.
- A silent, unintended change in behavior shows up as an unexpected diff against `testdata/golden`
  in code review, the same way the differential-build comparison caught regressions during the
  port — but now self-contained, with no second implementation required to diff against.
- The fixture still has to be kept buildable with a real Android SDK and Gradle (`update.sh`) for
  anyone who needs to change its sources and recompile `classes.zip`; the tests themselves don't
  need that toolchain, only the person updating the fixture does.
- This is deliberately narrower than testing against a large real app: it cannot catch a precision
  bug that only manifests at a scale or in a code shape the fixture doesn't happen to model. The
  README still recommends checking a change against a real project's build in addition to the
  golden tests, as a manual step, precisely because the fixture cannot replace that.

## Alternatives considered

- **Keep testing against one real, external Android project's compiled output, committed or
  fetched into this repository.** Rejected: ties every test run to that project's size, license,
  and continued availability, and makes it unclear which specific behavior a given test is
  protecting — a diff against a 35,000-class real app's output is much harder to read and reason
  about than a diff against a small, purpose-built scenario.
- **Keep the old prototype around as a differential-testing oracle.** This is what the port
  actually did (and what the repository's git history still shows), but it stops being viable once
  the tool is extracted on its own: the prototype isn't part of this repository, isn't meant to be
  shipped with it, and tying this repository's tests to a separate, unshipped implementation isn't
  a sustainable long-term test strategy for a standalone project.
- **Unit-test the walk functions directly, without going through the CLI's `--json` output.** Would
  catch the same logic bugs but would not exercise the thing actually being promised to users — the
  stable `--json` contract (0009) itself; running the real CLI and diffing its real output is what
  catches a contract regression (a reordered field, a changed exit code) as well as a walk-logic
  regression, in the same test.
