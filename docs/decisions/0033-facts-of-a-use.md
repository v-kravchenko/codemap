# 0033. What a use is to its symbol: facts, not a caller kind

- Status: accepted
- Date: 2026-11-02

## Context

A consumer lists the actions of a screen (what the UI calls on its ViewModel, the hooks a base class
calls, what runs when the screen opens) by asking `used-by` for each member of a class and guessing
from the names of the callers: a lambda named after a composable is UI, a caller in another class is
a hook. It asked for a kind of caller (`composable`, `fragment`, `viewmodel`, `base_class`, `init`),
the screen a use comes from, and the text of the button that calls it.

## Options

- **A caller kind**: one value per use from a fixed list. The list is one consumer's way of sorting
  its census; a use can be several at once (a lambda of a screen composable created in a base
  class), and every new consumer would want its own list.
- **Facts**: what the map knows about the code of a use, each as its own field; the consumer sorts.
- **The button's text**: the lambda reaching the call is handed down as a parameter and meets its
  string resource in another composable; it needs the flow of values, which the map does not keep,
  and naming an action for a tester is a verdict, not a fact.

## Decision

Facts. Each item of `used-by --json` gets (schema 4.7.0):

- `target`: which resolved symbol it uses (`used-by` takes several symbols);
- `source`: the method its code was written in; a lambda, function reference or coroutine counts as
  the method creating it, by the class file's `EnclosingMethod` (kept in the map since index 13),
  else by its compiled name; a lambda of `init` counts as `<init>`;
- `constructor`: that method is a constructor or a static initializer;
- `supertype`: its class is a supertype of the target's class (a base class calling a hook);
- `screens`: the screens whose own code it is (`{kind, name}`): the screen's class, a composable a
  route draws, or a private function of its file only those call.

When the method is not known (a class or field, a lambda of a field initializer) the fields are left
out, not guessed. `find --json` gives each class, method and field its JVM `visibility`, the repo
methods it `overrides` and whether it is `synthetic`; `find --owner` keeps the members of matching
classes. The text of a button is not taken (docs/limitations.md).

## Consequences

- `visibility` is the JVM's: a Kotlin `internal` member is `public`, a nested Kotlin `private class`
  is not `private` (its access is in `InnerClasses`, not read).
- `overrides` names repo methods only: an override of a framework method (`onCreate`) has none, the
  library not being in the map.
- The index version moved to 13: a map built by an earlier version fails a query until rebuilt
  (`codemap build`, or the build hook).
