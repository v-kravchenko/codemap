# 0007. `nav-path` as a Dijkstra walk with step costs

- Status: accepted
- Date: 2026-10-23

## Context

`impact` answers "what code runs because of this change", stopping the walk at the first screen it
reaches — it does not answer "how does a user reach this screen at all". A separate question came
up: given a screen, show every way a user can navigate to it from an entry point (an app launcher,
a deep link, a push notification).

This needs a different kind of edge than `impact` walks: "screen A opens screen B", built from
route registration and route-opening call sites (an object or constructor handed to a navigation
call, a class literal passed to start an Activity, an `<action>` in an XML navigation graph, an
Android manifest entry point), plus a notion of which Activity or module a screen is reachable
from ("hosted in").

Two complications came up that a plain unweighted BFS does not handle:

- **Shared code.** A central navigation controller or a base class's click handler can be the
  single code path that opens many different screens, depending on runtime data codemap cannot see
  statically. A shortest-unweighted-hops walk treats every one of those as an equally good, equally
  "direct" way to reach each target, which is misleading when some of those screens are reached
  only through heavily data-dependent branches. The number of screens below a node where this
  starts (a node shared by many screens) and that it is being asked about (a push/deep-link
  marker) needed a cost rather than a yes/no cutoff.
- **Push and deep-link entry.** A screen reachable only via a push notification handler or a deep
  link is "reachable" in the graph but is a materially different way to get there than an explicit
  in-app button tap. It was marked by looking for `Push`/`DeepLink` in the opening method's name,
  since there was no structural edge to key on otherwise.

## Decision

`nav-path` is a shortest-path (Dijkstra) walk over states of (screen, host), not a plain unweighted
BFS:

- An edge costs `own` (default 1) when a screen's own code opens the next one directly, `shared`
  (default 3, config key `nav_path.costs`) when the step passes through code shared by many screens
  (a "hub": a node with `hub_min` (default 10) or more screens reachable below it), and gets a
  `push` penalty (default 30) added when the opening method's name matches a configured push/deep-
  link marker.
- "Screen A opens screen B" (`opened_by`) is precomputed once, at build time, for every screen —
  not recomputed per `nav-path` query — because a screen's set of openers does not depend on which
  target screen is being queried.
- A pair "A opens B" is accepted when at least one of the shortest ways between them passes the
  host/module check (the host check decides whether the code doing the opening would actually run
  from a place that can reach B); when more than one shortest way exists, only one needs to pass.
  The path itself is treated purely as an explanation, kept separate from whether the flow is real
  — the same separation a static-analysis tool makes between the yes/no finding and its example
  trace.
- There is a per-pair cap (`shortest_ways`, default 64) on how many equal-cost paths are explored
  before giving up and reporting the pair as a capped case rather than hanging on a highly
  connected graph.

## Consequences

- `nav-path` output distinguishes a direct screen-to-screen step from one through shared hub code,
  and flags a push/deep-link-only path as a different, costlier kind of reachability — closer to
  what a developer or an AI agent reading the output actually wants to know than an undifferentiated
  "reachable in N hops" would be.
- Because `opened_by` is built once per screen rather than per query, `nav-path` queries are fast
  (tens of milliseconds) even on an app with many hundreds of screens, at the cost of doing that
  precomputation during every `build`.
- The push/deep-link detection is a name-pattern heuristic (matching configured marker words
  against the opening method's name), not a structural signal — it is only as good as the method
  naming convention in the project being analyzed, and is itself a config key
  (`nav_path.push_markers`) for that reason, per 0006.
- The "at least one shortest way" rule and the hub/push cost model were tuned against a real app's
  navigation graph and checked against hand-verified screen paths before being accepted; getting it
  wrong either hides real reachability or reports a data-dependent branch as if it were a normal,
  always-reachable step.

## Alternatives considered

- **Plain unweighted BFS/shortest-hop-count.** Rejected: treats a step through a shared hub
  (reachable only via a runtime-dependent switch) exactly like a direct call, which produced
  misleading "shortest" paths through generic navigation controllers that are not a meaningful
  description of how a user actually gets somewhere for a specific target screen.
- **Treat every shortest-length path as required to independently pass the host/module check,
  instead of "at least one".** Tried, and found to under-report: a path through a shared branch
  that happens to pick a different (but equally short) route than the one that passes the host
  check was wrongly excluded; switching to "at least one shortest way passes" recovered legitimate
  reachability that the stricter rule had dropped, with zero newly-introduced false positives on
  the hand-checked pairs re-verified after the change.
- **No cap on explored equal-cost paths.** Rejected after finding pathological fan-out through
  shared controllers would otherwise let the walk explore an unbounded number of equivalent paths;
  a per-pair cap with an explicit "left out" report was adopted instead of either hanging or
  silently truncating.
