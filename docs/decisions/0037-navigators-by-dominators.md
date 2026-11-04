# 0037. Navigators by dominators for graph and impact; nav-path keeps its hubs

- Status: accepted
- Date: 2026-11-04

## Context

`opened_by` takes as a hub of a way the lowest node of the walk's tree with `nav_path.hub_min` or
more screens below it (0007). When a node every screen calls (a state observer signing out) has
a navigator below it that ten screens pass, that navigator is the hub of those ten and the observer
is the hub of none: the screens calling the observer themselves get no hub at all. On a project of
969 screens 373 of the 465 ways into the sign-in screen were `direct`, though every one of them runs
through the observer; `graph`'s `via` and `impact`'s `opener` read those hubs.

`build` keeps since 0035 the dominators of each way (`shared`: the nodes every shortest way passes,
with how many screens share each), a fact that does not depend on the way picked.

## Options

- Hubs by dominators everywhere: a way's hub is its nearest dominator `hub_min` or more screens
  share, a screen being none.
- The same for `graph` and `impact` only, `nav-path` keeping the hubs of the tree.
- Leave the hubs as they are, `shared` telling the reader.

## Decision

`graph` (`via`, `hub`, `hubs`) and `impact` (which nodes are navigators, for `opener`) take a way's
navigator from its dominators when the query runs: the nearest one `hub_min` or more screens share,
a screen being none. The map and `build` are unchanged; `nav-path` keeps the hubs `build` chose.

A step into a navigator from another file crosses it even from another navigator: a change to a
sign-out navigator reaches the screens behind the observer calling it as `opener`. A change to the
navigator itself still crosses none.

## Consequences

- On that project 1,697 of 6,325 ways turn from `direct` to `opener` (the observer, navigators of
  user documents, of the dashboard), 10 the other way (a screen reaching its target through two
  openers of one navigator: no node all its ways pass); the navigators are 80, not 24. `impact` on
  60 shared nodes: 66 screens from `direct` to `opener`, none back. `nav-path`: unchanged.
- Dominators everywhere were tried and left out: with most navigators hubs, nearly every step of
  `nav-path` costs `shared`, and its cheapest way runs through the few that do not, such as a pop-up a
  server task opens, instead of the flow a user takes (checked by hand on 10 screens: 4 better, 6
  worse). Ranking the steps a user takes needs its own check against known ways (ROADMAP).
- A hub in `nav-path` (and in its `--json`) and a navigator in `graph` may differ for one way.
