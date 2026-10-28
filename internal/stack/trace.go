// Package stack reads a crash's stack trace, as Crashlytics exports it, logcat prints it or Java
// writes it, and an R8/ProGuard mapping.txt that turns an obfuscated one back into the source names.
package stack

import (
	"regexp"
	"strconv"
	"strings"
)

// Frame: one `at` line; Line 0 when the trace has none. Obfuscated: the frame as the trace had it,
// when a mapping renamed it (nil otherwise).
type Frame struct {
	Class, Method, File string
	Line                int
	Obfuscated          *Frame
}

// Exception: the thrown class and its message; Frames from the deepest call out.
type Exception struct {
	Class, Message string
	Frames         []Frame
}

// Trace: the exception and its causes, outermost first; the other threads of a dump are not read.
type Trace struct {
	Exceptions []Exception
}

var (
	// logcat's prefix, threadtime (`10-28 12:00:00.000  123  456 E AndroidRuntime: `) or brief (`E/AndroidRuntime( 123): `)
	logcatPrefix = regexp.MustCompile(`^(?:\d\d-\d\d \d\d:\d\d:\d\d\.\d+\s+\d+\s+\d+\s+[VDIWEF]\s+[^:]*?\s*: ?|[VDIWEF]/[^(:]*\(\s*\d+\): ?)`)
	frameLine    = regexp.MustCompile(`^at\s+([^\s(]+)\.([^.\s(]+)\(([^)]*)\)`)
	// an exception's first line: a qualified class, then `: message` or nothing; `Caused by` and
	// Crashlytics' `Fatal Exception:` before it. A thread of a dump (`main:`) has no dot or ends at the colon.
	headLine = regexp.MustCompile(`^(Caused by:? |Suppressed: |Fatal Exception: |Non-fatal Exception: |Exception in thread "[^"]*" )?` +
		`([A-Za-z_$][\w$]*(?:\.[\w$]+)+)(?:: (.*))?$`)
	moreLine = regexp.MustCompile(`^\.\.\. \d+ (?:more|common frames omitted)$`)
)

// Parse: the first trace in text; ok false when it has no exception with frames.
func Parse(text string) (Trace, bool) {
	var t Trace
	suppressed, framed := false, false
	for raw := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(logcatPrefix.ReplaceAllString(strings.TrimSpace(raw), ""))
		if line == "" || strings.HasPrefix(line, "#") || moreLine.MatchString(line) {
			continue
		}
		if m := frameLine.FindStringSubmatch(line); m != nil {
			if len(t.Exceptions) > 0 && !suppressed {
				e := &t.Exceptions[len(t.Exceptions)-1]
				e.Frames = append(e.Frames, frame(m[1], m[2], m[3]))
				framed = true
			}
			continue
		}
		m := headLine.FindStringSubmatch(line)
		switch {
		case m == nil && framed:
			return t, true // the trace is over: another thread, a log line
		case m == nil:
			continue // before the trace, or the rest of a message running over lines
		case m[1] == "Suppressed: ":
			suppressed = true
		case len(t.Exceptions) == 0 || strings.HasPrefix(m[1], "Caused by"):
			t.Exceptions = append(t.Exceptions, Exception{Class: m[2], Message: m[3]})
			suppressed = false
		case framed:
			return t, true // a second trace
		}
	}
	return t, framed
}

// frame: an `at` line's parts; the location is `File:N` or `File` (R8's `SourceFile`, `Unknown Source`, `Native Method` kept as given).
func frame(class, method, loc string) Frame {
	f := Frame{Class: class, Method: method, File: loc}
	if i := strings.LastIndexByte(loc, ':'); i >= 0 {
		if n, err := strconv.Atoi(loc[i+1:]); err == nil {
			f.File, f.Line = loc[:i], n
		}
	}
	return f
}
