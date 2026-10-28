package stack

import (
	"reflect"
	"strings"
	"testing"
)

// The way Crashlytics exports a crash: a header, `Fatal Exception:`, `Caused by` without a colon,
// then the other threads.
const crashlytics = `# Crashlytics -
# Application: com.example.app

Fatal Exception: java.lang.IllegalStateException: no data
       at com.example.app.Repo.load(Repo.kt:12)
       at com.example.app.Screen$onCreate$1.invokeSuspend(Screen.java:30)
       at android.os.Handler.dispatchMessage(Handler.java:125)
Caused by java.lang.NullPointerException: null
       at com.example.app.Store.read(Native Method)
       ... 2 more

main:
       at android.os.MessageQueue.nativePollOnce(MessageQueue.java)

DefaultDispatcher-worker-1:
       at java.lang.Object.wait(Object.java)
`

func TestParseCrashlytics(t *testing.T) {
	tr, ok := Parse(crashlytics)
	if !ok {
		t.Fatal("no trace")
	}
	want := []Exception{
		{Class: "java.lang.IllegalStateException", Message: "no data", Frames: []Frame{
			{Class: "com.example.app.Repo", Method: "load", File: "Repo.kt", Line: 12},
			{Class: "com.example.app.Screen$onCreate$1", Method: "invokeSuspend", File: "Screen.java", Line: 30},
			{Class: "android.os.Handler", Method: "dispatchMessage", File: "Handler.java", Line: 125},
		}},
		{Class: "java.lang.NullPointerException", Message: "null", Frames: []Frame{
			{Class: "com.example.app.Store", Method: "read", File: "Native Method"},
		}},
	}
	if !reflect.DeepEqual(tr.Exceptions, want) {
		t.Fatalf("got %+v", tr.Exceptions)
	}
}

func TestParseLogcat(t *testing.T) {
	text := `10-28 12:00:00.123  1234  1234 E AndroidRuntime: FATAL EXCEPTION: main
10-28 12:00:00.123  1234  1234 E AndroidRuntime: Process: com.example.app, PID: 1234
10-28 12:00:00.123  1234  1234 E AndroidRuntime: java.lang.RuntimeException: Unable to start activity
10-28 12:00:00.123  1234  1234 E AndroidRuntime: 	at a.b.c(SourceFile:3)
10-28 12:00:00.123  1234  1234 E AndroidRuntime: Caused by: java.lang.IllegalArgumentException
10-28 12:00:00.123  1234  1234 E AndroidRuntime: 	at com.example.app.Main.<init>(Unknown Source:7)
10-28 12:00:00.124  1234  1234 I Process : Sending signal. PID: 1234 SIG: 9`
	tr, ok := Parse(text)
	if !ok || len(tr.Exceptions) != 2 {
		t.Fatalf("got %+v", tr)
	}
	if f := tr.Exceptions[0].Frames[0]; f != (Frame{Class: "a.b", Method: "c", File: "SourceFile", Line: 3}) {
		t.Errorf("frame: %+v", f)
	}
	if e := tr.Exceptions[1]; e.Class != "java.lang.IllegalArgumentException" || e.Message != "" ||
		e.Frames[0] != (Frame{Class: "com.example.app.Main", Method: "<init>", File: "Unknown Source", Line: 7}) {
		t.Errorf("cause: %+v", e)
	}
}

func TestParseSuppressedAndSecondTrace(t *testing.T) {
	text := `java.lang.Error: first
	at a.A.f(A.kt:1)
	Suppressed: java.lang.Error: other
		at a.B.g(B.kt:2)
Caused by: java.lang.Error: cause
	at a.C.h(C.kt:3)
java.lang.Error: second
	at a.D.i(D.kt:4)`
	tr, _ := Parse(text)
	if len(tr.Exceptions) != 2 || len(tr.Exceptions[0].Frames) != 1 || tr.Exceptions[1].Frames[0].Class != "a.C" {
		t.Fatalf("got %+v", tr.Exceptions)
	}
	if _, ok := Parse("no trace here\nat all"); ok {
		t.Error("a text without frames is a trace")
	}
}

const mapping = `# compiler: R8
com.example.app.Money -> a.a:
# {"id":"sourceFile","fileName":"Money.kt"}
    long cents -> a
    1:4:java.lang.String format(long):18:21 -> a
    5:5:void check(long):30:30 -> b
    5:5:java.lang.String com.example.app.Text.join(long):7 -> b
    5:5:void check(long):31 -> b
    void reset() -> c
    1:9:void <clinit>():0:0 -> e
    1:30:java.lang.Object run():28:28 -> f
    void put(int) -> d
    void put(long) -> d
    1:1:void <init>():10:10 -> <init>
com.example.app.Text -> a.b:
# {"id":"sourceFile","fileName":"Text.kt"}
`

func TestRetrace(t *testing.T) {
	m, err := ReadMapping(strings.NewReader(mapping))
	if err != nil {
		t.Fatal(err)
	}
	frames := func(f Frame) []Frame {
		out := m.Retrace(f)
		for i := range out {
			out[i].Obfuscated = nil
		}
		return out
	}
	cases := []struct {
		in   Frame
		want []Frame
	}{
		{Frame{Class: "a.a", Method: "a", Line: 3}, []Frame{{Class: "com.example.app.Money", Method: "format", File: "Money.kt", Line: 20}}},
		{Frame{Class: "a.a", Method: "b", Line: 5}, []Frame{ // inlined: the body first, then where it was inlined
			{Class: "com.example.app.Money", Method: "check", File: "Money.kt", Line: 30},
			{Class: "com.example.app.Text", Method: "join", File: "Text.kt", Line: 7},
			{Class: "com.example.app.Money", Method: "check", File: "Money.kt", Line: 31},
		}},
		{Frame{Class: "a.a", Method: "c", Line: 9}, []Frame{{Class: "com.example.app.Money", Method: "reset", File: "Money.kt"}}},
		{Frame{Class: "a.a", Method: "d"}, []Frame{{Class: "com.example.app.Money", Method: "put", File: "Money.kt"}}},               // overloads: one name
		{Frame{Class: "a.a", Method: "e", Line: 4}, []Frame{{Class: "com.example.app.Money", Method: "<clinit>", File: "Money.kt"}}}, // 0: no line
		{Frame{Class: "a.a", Method: "f", Line: 17}, []Frame{{Class: "com.example.app.Money", Method: "run", File: "Money.kt", Line: 28}}},
		{Frame{Class: "a.a", Method: "z"}, []Frame{{Class: "com.example.app.Money", Method: "z", File: "Money.kt"}}},
		{Frame{Class: "a.a", Method: "<init>", Line: 1}, []Frame{{Class: "com.example.app.Money", Method: "<init>", File: "Money.kt", Line: 10}}},
		{Frame{Class: "android.os.Handler", Method: "post", File: "Handler.java", Line: 4}, []Frame{{Class: "android.os.Handler", Method: "post", File: "Handler.java", Line: 4}}},
	}
	for _, c := range cases {
		if got := frames(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %+v", c.in, got)
		}
	}
	if f := m.Retrace(Frame{Class: "a.a", Method: "a", Line: 1})[0]; f.Obfuscated == nil || f.Obfuscated.Class != "a.a" {
		t.Errorf("obfuscated frame not kept: %+v", f)
	}
}
