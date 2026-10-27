# Contributing

Issues and pull requests are welcome. For a change larger than a fix, open an issue first, so the
approach is agreed before the code.

- How the code is laid out, the checks to run and the rules a change keeps: [AGENTS.md](AGENTS.md)
  (written for people and coding agents alike).
- `tools/check.sh` must pass; CI runs it on every pull request, and the tests on macOS and Windows.
- A change to an answer of the CLI rewrites `testdata/golden` (`go test ./cmd/codemap -run TestGolden
  -update`); say in the pull request why the answer changed.
- codemap knows no particular project: what differs between projects goes into `codemap.yml` and the
  presets, never into the code (docs/decisions/0006).
- A wrong answer on your project is best reported with a small Kotlin or Java example that shows it;
  `testdata/navapp` is where such examples become tests.
- Commit messages: one sentence on what changed and why it matters, no prefixes.
- A user-visible change adds a line under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md).
