# 0028. A screen a push service opens has a push entry

- Status: accepted
- Date: 2026-10-28

## Context

`nav-path` starts at the manifest's entries. A screen only a notification opens ended at `outside`
(code that is not a screen opens it), and `screens --unreachable` listed it. 0027 put the services
and receivers of the manifests into the map.

## Options

- **Push as a cost**: `nav_path.push_markers` already makes a step through push handling dear;
  it needs the push code between two screens, not before the first.
- **Push as an entry**: a component whose intent filter takes a push (`nav_path.push_actions`, a
  preset gives Firebase's) is a start; a screen its code opens is an entry of kind `push`.

## Decision

The second. When build walks up from what opens a screen (`opened_by`), the components taking a
push that walk meets before any screen give the screen a `push` entry, the component's name as
its detail; the entry is in the map with the manifest's. It sorts after `intent` and before
`base`: a launcher way stays the cheapest when there is one.

## Consequences

- `nav-path --entry push`, `screens --unreachable` (a screen only a push opens is reached) and
  `diff` (entries) see it.
- On a 78-module project the push service opens the splash Activity, which routes on by extras:
  the splash got the push entry, no other screen did (no screen was only `outside` because of it).
- A push action not in `push_actions`, or a push handled in a library's service, gives no entry.
