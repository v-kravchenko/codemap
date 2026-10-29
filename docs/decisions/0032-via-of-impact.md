# 0032. How a change reaches a screen: `via`, the walk unchanged

- Status: accepted
- Date: 2026-10-29

## Context

`impact` answers whether a screen can reach the change, and on shared code that answer is wide: a
model of one message type reaches every screen reading any message, through the registry that
names all of them; a session helper reaches every Activity, through the base class observing it;
the internals of a flow reach every screen that opens the flow, through the navigator they share.
A tester checking a flow wants those screens; a consumer deciding by the answer (a question per
screen whose behaviour may have changed) gets one false question per screen it did not mean.

## Options

- **Narrow the walk by default**: stop at bases, registries and navigators. The answer a tester
  relies on loses true screens (the screens opening the flow do open it differently), and every
  consumer and golden answer moves.
- **Stop nodes in `codemap.yml`**: classes and methods the walk does not cross. Each project keeps
  the list by hand from nothing, and a screen past one is gone, not told apart.
- **A label per screen**: the screens stay; each says what the change reaches it through, found from
  the map; a consumer filters by it, `--direct` filters for the CLI.

## Decision

The label. Each screen of `impact`, `changes` and `crash` gets `via` (schema 4.6.0), the strongest of
its ways, in this order:

- `direct`: through code of its own and code it calls;
- `opener`: only through a navigator many screens open others through, entered from another file: a
  hub of `opened_by` (`nav_path.hub_min`), or any method of a class holding one, not of a file of
  top-level functions (a method of the navigator only one screen calls routes like the rest): what
  the screen opens changes;
- `hub`: only through a method naming `impact.hub_subtypes` (20) or more classes below one supertype
  of the project, entered from one of those classes;
- `base`: only through code of a class with `impact.base_subclasses` (10) or more subclasses, run for
  no instance in particular (reached through a singleton, not through the subclass's own state),
  into a subclass.

The walk is rerun with these steps cut, from the strictest: a screen reached with none of them is
`direct`, with openers only `opener`, with openers and hubs `hub`, else `base`. A change to the
navigator, the hub or the base itself crosses nothing and is `direct`. A plain class with many
dependencies is no hub: the classes it names must share a supertype. Without `--direct` the answer
is the same as before but for the field.

## Consequences

- The walks cost three more runs of the impact walk; on a project of ~35k classes a query takes the
  same time, the walk being a small part of it.
- `via` names a kind of way, not whether the screen behaves differently: a screen routing a push by
  code of its own, not through a navigator other screens share, is `direct` even when it only opens
  the flow the change is in, and a screen whose only way crosses a navigator and a base is `base`.
- 0.12.1 widened the navigator from the hub method to its class: in 0.12.0 a screen calling a method
  of the navigator no other screen calls (a splash routing a push) was `direct`, and a change inside
  the navigator reaching its hub method made the screens behind it `opener`.
- The thresholds are settings; `TestVia` checks the labels on the fixture with thresholds its few
  classes reach.
