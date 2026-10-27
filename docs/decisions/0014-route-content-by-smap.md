# 0014. A route's content: the composables called from where it is registered, by SMAP

- Status: accepted
- Date: 2026-10-27

## Context

A Compose route is a screen by its route (`CreateRoute.Confirm`), while people know it by the
composable it draws (`CreateConfirmScreen`); `screens CreateConfirmScreen` found nothing. The map
already links a route to the lambda it registers (`ROUTE_CONTENT`), but that lambda calls more than
the screen: a project's inline wrapper (`screen<VM> { ... }`) is copied into it with its own
composables (a state observer, a hotline banner), and a lambda without captures sits in
`ComposableSingletons` behind a getter. Which call is "the screen" is a fact of the source, not of
the call graph.

## Decision

- `build` records, for each route, the repo composables its content calls from code written where
  the route is registered (`snapshot.ScreenContent`, index version 9): a call counts when its line
  is the class's own, or when the Kotlin SMAP maps it to a source class in a file that registers the
  route; lines SMAP maps to the class a copy was made from (the wrapper's own lambda) do not.
  Lambdas calling themselves again on recomposition are left out.
- The parser keeps the line of every reference (`Ref.Line`, from the LineNumberTable) and the SMAP's
  ranges (`Class.SmapLines`, `SmapOrigin`).
- `screens` shows them as `content` (schema 3.5.0) and its FILTER matches them.
- A route's hosts are searched from the code registering it too (`ROUTE_HOST`), and up from a
  lambda's method to the method its class is made in: a route registered in a section function
  called inside an inline `navigation {}` had none.

## Consequences

- 398 of 399 routes on the work project name their screen composable (the one left is `screen(R) {}`),
  374 exactly one; the rest also name what the registration itself calls (`collectAsStateWithLifecycle`,
  an analytics effect), which is the code there.
- Without a LineNumberTable (stripped classes) every call counts as the route's own.
- A composable from a library is not listed: only repo code is in the map.

## Alternatives considered

- **Query time, by call frequency** (a composable called from most routes is plumbing): no format
  change, but a guess that drifts with the project and cannot tell a shared screen (`PdfScreen`, 24
  routes) from a wrapper's part.
- **Same module as the route**: drops a screen kept in a shared UI module.
- **Line past the end of the file** (Kotlin numbers inlined lines after the file's last): fails for
  a copy an inline function made, whose own lines are the wrapper's file and whose written content
  is the inlined part.
- **The source class must not be compiler-made**: fails for content written inside an inline
  function's lambda (`flowExtended<VM>() { screen<VM> { ... } }`).
