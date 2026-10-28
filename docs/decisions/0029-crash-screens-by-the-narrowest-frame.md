# 0029. A crash's screens are those of its own frame reaching the fewest

- Status: accepted
- Date: 2026-10-28

## Context

`crash` reads a stack trace and answers where it ran and for which screens. Each own frame can
be walked as `impact` walks a symbol, but frames disagree: on a 78-module project the deepest own
frame of five real Crashlytics crashes was a shared helper (a base fragment's state handler, a
dialog of the UI kit, a logout helper) reaching from 53 to over 800 screens, while another own frame of the
same trace (a ViewModel's coroutine, a fragment's `onViewCreated`, the route's composable) reached
one or two. A crash in a worker, a service or `Application.onCreate` reaches no screen at all
(0027).

## Options

- **The deepest own frame**: where it threw; for a shared helper, every screen using it.
- **The outermost own frame** (the entry): often the screen's lifecycle method, but in a coroutine
  or a posted callback it is the coroutine's body, and a shared base (`BaseFragment.onViewCreated`)
  can sit below the screen's frame.
- **The intersection of the frames' screens**: the frames all ran for one screen, so it is in each
  set; a gap of the walk in one frame empties it.
- **The frame reaching the fewest screens and components**: the most specific code the crash ran
  through.

## Decision

The last: the resolved frame with the smallest non-zero reach (screens plus components), the
nearest the crash on a tie. Its walk lists the screens and components, with paths, as `impact`
does. On the five crashes it gave the screen the trace ran for in each one that had a frame
of it: what the intersection would give, without its empty answer for a gap. The answer still
names where it threw (`crashed_at`) and the entry (`entry`), so the shared helper is not hidden;
`frames[].reach` shows each frame's count.

The trace is read whole, `Caused by` included: where it threw is the deepest own frame of the
innermost cause that has one, the entry the outermost own frame of the outermost exception that has
one. An R8 `mapping.txt` is read in the same command (`--mapping`): Crashlytics deobfuscates its
own traces, logcat and the Play Console do not.

## Consequences

- A crash whose own code runs only in a coroutine or callback some shared class launched lists
  every screen that class reaches (docs/limitations.md): the trace names no narrower code.
- A frame is found by class and line, so a trace of another build finds the method by name or its
  class only; a map of other sources answers for them.
- Frames are walked one by one (each distinct frame once): 0.2–0.7 s on the 78-module project.
