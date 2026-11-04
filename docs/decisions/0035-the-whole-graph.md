# 0035. The whole graph: the build's ways, with facts of the code on each

- Status: accepted
- Date: 2026-11-04

## Context

A consumer drawing an interactive map of an app (every screen, its neighbours, the flows) needs the
whole graph of screens as data. `graph SCREEN` draws one screen's neighbourhood for Mermaid: each
node once, a navigator of many screens folded into `+N`, at most `graph.max_edges` arrows, nodes
named by their short name. 969 calls of it on a large project took about 3 minutes and still left
out what it folds; two screens of one name could not be told apart; nothing said whether a way is
the screen's own or one every screen inherits (logout, a technical-work dialog), or where in the code
it is.

## Options

- **A command of its own** (`export`): a second command over the same rows.
- **`graph` without a screen**: one command, as `nx graph`, `go mod graph`, dependency-cruiser:
  what to take (a focus or all), how to walk, how to print.
- For the ways through code many screens share: a new `via` value with a threshold in codemap.yml,
  a new rule for hubs, or a fact on the edge.
- For the ways through a base class (its code opens a screen for every subclass): an edge per
  subclass, or a row per base.

## Decision

`graph` without `SCREEN` prints every `opened_by` row (schema 5.0.0): no way merged, dropped or cut;
the Mermaid limits stay with the text. Nodes and edge ends are ids (a screen's `target`), the name
is for showing; the focused graph takes the same shape.

Each edge carries facts of the code, never a threshold of ours:
- `via` as `impact` names it (`direct`, `opener` through a navigator, `base` through inherited code),
  so a word means one thing in every command;
- `shared`: the dominators of the way in the graph of shortest ways (nodes every shortest way from
  the source passes), each with how many source screens share it, computed by `build`. A count of
  the ways codemap happened to pick would move with any edit; dominators do not depend on the pick.
  Whether 365 sources make a way global is the reader's call;
- `location`: the line of the call that opens the screen, and `source_location`, the first call on
  the source's side. `build` keeps the first line of each edge's code (`out.line`), mapping a lambda
  an inline function copied through the SMAP, else the line of the inline call.

A base class whose code opens screens is one row (`bases`: its screens, what it opens), a hyperedge
as JSON Graph Format v2 has them: 9 rows instead of 9,039 edges on a large project.

## Consequences

- A whole map of 969 screens and 6,325 ways prints in 0.1 s; `build` takes about 10% longer and 5%
  more memory (dominators, lines), the map 3% more.
- A way through shared code is still `direct` for `via` (as for `impact`): `shared` tells it apart.
- The rule for hubs (the lowest node with `hub_min` screens below, on one shortest way) is
  untouched; `graph` and `impact` take their navigators from the dominators since 0037.
