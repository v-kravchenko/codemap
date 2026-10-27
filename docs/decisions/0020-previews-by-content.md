# 0020. A preview goes with the screen whose own composables it draws

- Status: accepted
- Date: 2026-10-27

## Context

A screenshot job renders previews; a reviewer wants to know which screen each shows and which
changed screens no preview shows. The map related a screen to the previews in the files of the
composables it calls, but no query read it, and a file often holds several screens (a NavHost
file with its destinations, the fixture's `SectionsApp.kt`): each preview went with all of them.

## Options

- **By file** (what the map had): cheap, wrong wherever a file holds two screens.
- **By file, only when the file holds one screen**: never wrong, silent for shared files, so
  "no preview" would not mean none.
- **By content**: a preview's content is the composables it draws (through its lambdas); it goes
  with the screens drawing them.

## Decision

By content, scoped so the composables shared by every screen do not link everything:

- a preview's candidates are the composables it draws of its own file, else of its module (a
  theme getter of a shared module is out);
- a screen draws the composables of its own module it calls (a route: its registration's content)
  and what those call in their own file (a screen's private `ScreenContent`);
- of several candidates, those the fewest screens draw: the screen's own composable rather than a
  component.

On a 78-module project 262 of 825 previews go with a screen: 202 with one, 57 with two (an
Activity and the route it hosts), none with more than 7.

## Consequences

- `screens`, `impact` and `changes` list `previews`; `--no-preview` lists what no preview shows.
- A preview of a design-system component goes with no screen; a component of the screen's own
  module drawn by it alone counts as its preview.
- The map's relation changed meaning, so the index version moved to 10.
