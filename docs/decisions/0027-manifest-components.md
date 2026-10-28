# 0027. Manifest components and workers: reached, listed apart from screens

- Status: accepted
- Date: 2026-10-28

## Context

`impact` and `changes` stop at screens. A change reaching only a push service, a boot receiver, a
worker or `Application.onCreate` answered `Screens: 0`, as if it touched nothing a user meets. A
stack trace input and a push entry for `nav-path` need these classes as nodes too.

## Options

- **New screen kinds** (`service`, `receiver`, ... in `screens[]`, out of the default `--kind`):
  little code, but a consumer of `screens[]` with `--kind all` gets classes that draw nothing, and
  the walk stops at a screen, which would cut what an Application's getters reach.
- **A list of their own** (`components[]` next to `screens[]`): `screens[]` keeps its meaning and
  bytes; a component is marked where the walk meets it and the walk goes on.

## Decision

The second. The manifests of the modules name the `application`, `service`, `receiver` and
`provider` classes; a class of the map they name is a component (a library's, such as
`FileProvider`, is not in the map). A manifest does not declare workers: a concrete class below
`components.worker_bases` (a preset gives WorkManager's bases) is a `worker`. A walk reaching a
member of the class, or of the top-level class holding it, lists the component with the path to it.

## Consequences

- On a 78-module project: one application and one service (the push service); impact of 13
  repositories gave the same `screens[]` and `affected` as before.
- `components` is a new field of `impact` and `changes` and of `build --info` (schema 4.2.0); the
  map has a new column, so the index version moved to 12.
- `diff`, `nav-path` and `screens` do not read components yet; a push entry (`--entry push`) and a
  stack trace input build on them.
