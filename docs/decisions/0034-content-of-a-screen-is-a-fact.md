# 0034. What an Activity or fragment draws is a fact, not its own code

- Status: accepted
- Date: 2026-11-04

## Context

A route's `content` (`screens`) lists the composables its registration calls; an Activity or
fragment drawing Compose had none. A consumer building a catalog of screens from `previews` asked
for the same `content` there, and for previews of the composables a screen calls from another module
(a body several screens share: a photo intro drawn by 8 fragments of 3 modules).

`content` feeds two answers that say more than what is called: `previews` (this picture is this
screen) and `used-by` `screens` (this code is the screen's own, ADR 0033).

## Options

- **One rule for all**: an Activity's or fragment's `content` counts as its own code, as a route's
  does: a shared body's previews and uses go with every screen drawing it.
- **content as a fact**: list what the screen's own code calls, of any module; previews and own code
  stay with what a module keeps to its screens.
- **Nothing for Activities and fragments**: as before.

## Decision

`content` as a fact (schema 4.8.0): the repo composables the methods of the screen's class call that
its own code does not call (`onCreate`, an overridden composable its base calls), through their
lambdas, those its routes draw left out, a preview of the class not counted. Not what those call: the
screen's private body is listed, not its parts.

A preview of a body another module keeps does not go with the screens drawing it: a shared body
drawn with sample data is not a picture of any one of them, and 8 equal pictures for 8 screens
mislead more than none. The module is the proxy for shared: a composable of another module drawn by
one screen alone is left out too (docs/limitations.md). `used-by` `screens` keeps taking own code
from routes and previews only.

## Consequences

- `screens` shows what an Activity or fragment draws, also where it has no preview (why: its body is
  another module's); the filter finds it by that composable.
- An Activity drawing a composable that hosts its routes (an app shell with a NavHost) lists that
  composable; one registering routes in its own lambdas lists none of theirs.
- A route's `content` still counts as its own code, a shared body included: a route registered
  with another module's composable is the case this leaves as it was.
