# 0008. General type-based rules, never project rules

- Status: accepted
- Date: 2026-10-26

## Context

Precision problems kept surfacing where a walk through bytecode produced a technically-correct but
practically-useless answer: a change to one view model's field showed up as affecting every screen
in the app, because the walk followed a generic base class or a dependency-injection framework's
generated glue code as if it were ordinary application code.

The sharpest example: a base Activity/Fragment class's `initViewModel()` obtains its view model
through a generic provider interface (`ViewModelProvider<VM>`-shaped), parameterized by the type
variable of the base class. Querying the impact of an unrelated view model or repository showed up
to several hundred screens affected, because the walk reached that generic call and, unable to
narrow it, treated every class that ever instantiates that base class as a possible destination.

A second app (a single, differently-structured app using a different DI framework, deliberately
used as a check that fixes generalize) was used specifically to catch cases where a rule had been
tuned to one project's shape without the maintainer noticing.

The fix that resolved the generic-provider case: read the class file's `Signature` attribute (the
generic type information javac/kotlinc keep beyond the erased descriptor) for classes and fields,
and when an injected field's declared generic signature matches a class's own generic supertype
parameter exactly (no unresolved type variable, no wildcard, and not a `java.*`/`kotlin.*`
supertype or a lambda/anonymous class), treat that field as evidence for the owner of the generic
instance — narrowing "who could this `VM` be" down from "everyone who creates anything shaped like
`ViewModelProvider<VM>`" to "whoever's own generic supertype argument is exactly this `VM`". A
second, general rule: when an instance is produced only by dependency injection (an injected field
of a matching type, or a factory method a DI framework calls), the true owners are those holder
sites — not every subclass that could theoretically be instantiated by application code.
A third, unrelated general rule caught in the same audit: a dialog's lifecycle callback inherited
from a base class, invoked externally only through `.show(...)`, should be attributed to "this
call opens the dialog", not to "this call runs the dialog's inherited method" — but only for
dialogs; applying the same collapsing rule to a base Activity's open-from-elsewhere methods
produced wrong answers, because an Activity's own navigation code legitimately calls its own
methods from its own routes.

Dependency-injection framework glue — generated factory classes, generated members-injectors,
generated Hilt modules, Dagger-generated component/subcomponent classes — needed to be recognized
and excluded from these walks for the same reason: it is not application code a developer wrote,
and walking through it the same way as application code produces explosive, meaningless fan-out
(one generated component class alone produced over a hundred spurious "opens" edges onto an
Activity in one audit).

## Decision

Every precision fix is a general rule based on what the bytecode itself says (a type signature, a
modifier, a generated-name pattern, a call-site shape), never a rule keyed to a specific project's
class or package names.

- DI-generated glue classes are recognized by name patterns that frameworks are contractually
  required to produce (`_Factory`, `_Factory_Impl`, `_MembersInjector`, `_GeneratedInjector`,
  `_HiltModules*`, `_ComponentTreeDeps`, `_HiltComponents`, `_Provide*Factory`, `_Binds*`, a
  `Dagger`-prefixed component class, anything under a `hilt_aggregated_deps` package or the
  `dagger` package itself) plus the `dagger/Module` and `dagger/hilt/InstallIn` annotations — not
  by the name of any one project's classes.
- A call through a generic holder type is narrowed by matching the class file's own `Signature`
  attribute against the generic argument actually in play, with no fallback that assumes a
  specific base class name.
- A collapsing rule (treat an inherited callback as "caller opens the screen" rather than "caller
  runs the screen's inherited method") is scoped to the general case where it is sound — a dialog,
  which by the Android framework's own contract is activated externally through `show()`-family
  calls — and explicitly not generalized to Activities, where the same shape of rule produced wrong
  answers because an Activity's own code can legitimately call its own inherited methods as part of
  normal navigation.
- Every fix is checked for an unexpected drop in recall or an unexpected jump in extra
  (non-benchmark) results on the full benchmark suite before being accepted, and re-verified against
  a second, structurally different Android app to catch anything that happened to work only because
  of one project's conventions.

## Consequences

- The tool can be pointed at any Android project using Hilt or Dagger (or neither) without a config
  option to turn on "understand this framework's glue" — it is baseline behavior, because the
  pattern is the framework's own generated-code convention, not a guess about one project.
- The generic-holder narrowing fix measurably changed results on both apps it was checked against:
  on the project it was built for, a regression comparison against the version before the fix
  showed the vast majority of differences were pure DI-glue noise removed (over a hundred spurious
  call chains eliminated), with a smaller number of genuine behavior changes (both added true
  positives and removed false positives) from the generic narrowing itself.
- A new precision bug found in one project's codebase is fixed by asking "what general, type-level
  fact distinguishes the good case from the bad case", not "what's special about this project's
  class names" — and the fix is expected to be re-verified on an unrelated codebase before being
  trusted.
- This raises the bar for what counts as an acceptable fix: a rule that only happens to work
  because of a naming convention one project follows is not accepted, even if it fixes the
  immediate symptom.

## Alternatives considered

- **Hard-code the base class names of the framework where the precision bug was found.** Rejected:
  would fix the symptom on one project and silently fail to generalize, repeating the mistake 0006
  already corrected elsewhere in the tool (hard-coded navigation and screen-base-class names).
- **Collapse every "inherited callback with no overriding caller" case the same way, for any screen
  kind.** Tried for Activities as well as dialogs; produced wrong results, because an Activity can
  legitimately call its own inherited navigation methods as ordinary application code, unlike a
  dialog's external `show()` contract. The rule was scoped back to dialogs only once the difference
  was found.
- **Treat every instance of a type reachable through a generic call as a possible owner (no
  narrowing at all).** This was the original, pre-fix behavior, and is what produced the several-
  hundred-screen false-positive explosion that motivated the Signature-attribute fix in the first
  place.
