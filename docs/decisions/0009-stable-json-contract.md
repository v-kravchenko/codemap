# 0009. A stable, versioned `--json` contract

- Status: accepted
- Date: 2026-10-23

## Context

The intended consumers of this tool are not only a developer at a terminal: a CI job commenting on
a merge request, a label-setting integration, a test-management system, and an AI agent all need
to consume its output programmatically. The stated principle from the start: the CLI is the
analysis engine producing a stable output; every integration (a PR comment, a label, a database
row) is a thin adapter over that output, so a new integration idea never requires a core change.

That only works if the output contract is something adapters can actually rely on release to
release, and if a caller can tell the difference between "this run found nothing affected" and
"this run failed".

## Decision

Every command has a `--json` output mode with:

- A `schema_version` field, semantically versioned, separate from the tool's own version — so an
  adapter can keep working across tool upgrades that don't change the shape it depends on, and can
  detect a breaking change by major-version bump.
- A `meta` object: the tool version, the index (map) version, the git commit the map/index was
  built from (when available) and whether it's dirty, a content hash of the inputs, whether the map
  is stale relative to its inputs, which ref(s) were compared, and the run's start time and
  duration.
- Deterministic, byte-identical output for the same inputs: the same query against the same map
  produces the exact same bytes every time (aside from `meta.run`'s timestamp/duration), so results
  can be diffed between runs or stored for later comparison without normalization.
- Exit codes that distinguish real states: `0` for success, `3` for "the map is stale relative to
  the source it would need to answer accurately, and `--strict` was given" (so a CI gate that
  cares about correctness can fail loudly instead of silently answering from stale data), and a
  general error code (`2`) for a genuine failure (bad arguments, a file that can't be read). A
  result of "nothing affected" is a normal, successful `0`-exit response with an empty result, never
  something that looks like a failure — callers must be able to tell "no impact" from "the tool
  broke".

## Consequences

- An external adapter (a CI comment bot, a label-setter, a database importer) can be written once
  against the schema and keep working across tool upgrades that bump only the tool's own version,
  not the schema's.
- A `--strict` CI gate can be configured to fail the pipeline specifically when the map is
  answering from stale information, without that looking identical to "the change really does have
  no impact".
- Determinism (same bytes for the same inputs) means golden-file testing (0011) and CI result
  diffing both work without special-casing timestamps anywhere except the one documented `meta.run`
  field.
- A schema or exit-code change is a deliberate, documented change — not an incidental side effect
  of an unrelated refactor — because adapters outside this repository depend on it holding still.
- Keeping the contract stable costs real engineering discipline: a convenient internal refactor
  that would change field order, add an undocumented field, or alter how ties are broken in a
  result list has to go through the same "is this a deliberate, versioned change" bar as an
  intentional schema change.

## Alternatives considered

- **No formal schema version, just "whatever the current JSON looks like".** Rejected: the stated
  purpose of a stable CLI contract is that *adapters don't have to be rewritten on every CLI
  release*; without an explicit version, any shape change is indistinguishable from a breaking
  change, which pushes the versioning problem onto every consumer instead of solving it once.
- **Let the output order vary with hash-map iteration order or OS-level nondeterminism.** Early
  development hit exactly this (an id or ordering that depended on process-level hash-seed
  randomization), which was treated as a bug and fixed by sorting explicitly wherever order could
  otherwise vary — determinism was not an afterthought layered on top, it is load-bearing for the
  golden tests (0011) and for any CI result comparison.
- **Signal "no impact" with a non-zero or otherwise special exit code.** Rejected: conflates a
  correct, successful analysis that happens to find no effect with an actual tool failure, which
  is exactly the ambiguity the exit-code design was meant to avoid.
