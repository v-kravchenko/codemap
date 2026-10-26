# 0012. `nav-path --via` as chained walks, `--ways` by the fork

- Status: accepted
- Date: 2026-10-26

## Context

`nav-path` shows the cheapest way to a screen. A tester often needs another one: through a given
screen (the FAQ from the support screen, not from the profile), or every way a user can open it,
to pick the one a test case covers. Ways are walks over states (screen, host) with step costs
([0007](0007-nav-path-cost-model.md)), so "another way" has to respect hosts and costs too.

## Decision

- `--via A --via B` chains walks: from the target back to B (ending only at B), from that state of B
  back to A, from that state of A to an entry. Each part is the cheapest one; the answer has the
  same shape as a plain `nav-path` for that one way.
- `--ways` finds the fork: going back from the target along its cheapest way, the first screen that
  more than one screen opens (FaqActivity for FaqFragment, which only FaqActivity opens). For each
  screen that opens the fork it runs `--via SCREEN --via FORK`, with the step into the fork being the
  screen's own, and lists the ways by entry kind and cost.

## Consequences

- No new walk: the parts reuse the Dijkstra walk with limits (`from`, `direct`), so costs, hosts,
  push steps and hints mean what they mean in a plain `nav-path`.
- A part keeps the host state the part before ended in; with several hosts of a via screen the
  answer is the cheapest way through that state (`docs/limitations.md`).
- `--ways` answers the question a tester asks ("from which screens can the user open it") in one
  short list; openers before the fork are not listed, `--via` reaches them.

## Alternatives considered

- **One walk over states (screen, host, via screens passed).** Exact for every host, but it
  multiplies the states by the via count and needs its own reporting; the chained walks give the
  same answer whenever a via screen has one host, which is the common case.
- **k cheapest ways (Yen's algorithm).** The next cheapest ways mostly differ in a host or a shared
  hub, not in a screen the user sees, so the first ten ways can all be the same tap sequence. Ways
  through each opener of the fork differ in what the user does by construction.
- **One way through each direct opener of the target.** For a screen hosted in one Activity (a
  fragment of a navigation graph) the only opener is that Activity; the fork moves past it to where
  the user's choices are.
