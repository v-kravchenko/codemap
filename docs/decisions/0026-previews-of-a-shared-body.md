# 0026. A preview of a body screens share goes with its file's screen

- Status: accepted
- Date: 2026-10-28

## Context

0020 gives a screen the composables it calls and what those call in their own file. A screen's
composable often only fills a body written once for a few screens in another file of the module
(`SignboardPhotoScreen` calls `BaseUploadGuideScreen`), and its file's preview draws that body:
the preview went with no screen. Composables returning a value (a theme's `colors` getter,
`remember*`) counted as drawn, so a preview could go with a screen by a color getter both read.

## Options

- **What the screen's composable calls in its module, all screens drawing it**: the preview of
  one guide goes with the three guides drawing the body, the spread 0020 removed.
- **As above, and of screens tied, those with a composable in the preview's file**: a preview
  written next to its screen goes with it; one in a file of its own, with every screen drawing it.
- **A tie broken by the preview's package too**: tried on a 78-module project, it gave the
  Activity hosting a NavHost (whose destinations are in its package) rather than the routes, and
  settled no preview the file did not.

## Decision

The second. A screen draws what its composables call in its module (not only in their file). Of
the screens tied for a preview, those drawing a composable of the preview's file, when there are
any. What a route draws is the route's, not its host's. A composable call counts only when it
returns `V`: one returning a value draws nothing, in a preview's content and a route's.

On a 78-module project 317 of 535 previews go with a screen (279 before); 97 screens and
previews lost a getter or `remember*` from `content`.

## Consequences

- A preview in a file of its own drawing a body several screens share goes with all of them.
- A route's content is not drawn by the Activity or fragment whose NavHost registers it (in its
  own lambdas, or in the copy an inline wrapper such as `screen<VM> {}` leaves in its classes):
  0020 let a preview go with both ("57 with two"); now 293 of 317 go with one screen, 8 with two.
- A route registered in a `ComposableSingletons` lambda reads its content's field without the
  getter; that read is its content too (such routes had none).
- The map's relation changed meaning, so the index version moved to 11.
