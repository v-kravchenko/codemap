package classfile

import (
	"archive/zip"
	"encoding/binary"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/fixture"
)

// fixtureClasses: the class files of the fixture app, in the zip's order.
func fixtureClasses(t testing.TB) [][]byte {
	zr, err := zip.OpenReader(filepath.Join(fixture.TestdataDir(), "navapp", "classes.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var out [][]byte
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".class") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, data)
	}
	return out
}

func TestParseFixture(t *testing.T) {
	KeepStrings = true
	defer func() { KeepStrings = false }()
	classes := fixtureClasses(t)
	if len(classes) < 50 {
		t.Fatalf("%d classes in the fixture", len(classes))
	}
	methods := 0
	for i, data := range classes {
		c, err := Parse(data)
		if err != nil {
			t.Fatalf("class %d: %v", i, err)
		}
		if c.Name == "" || c.Super == "" && c.Name != "java/lang/Object" {
			t.Errorf("class %d: name %q, super %q", i, c.Name, c.Super)
		}
		methods += len(c.Methods)
	}
	if methods == 0 {
		t.Error("no methods parsed")
	}
}

// A copy an inline function made gives its lines to the classes they were written in.
func TestSmapRanges(t *testing.T) {
	smap := "SMAP\nNav.kt\nKotlin\n*S Kotlin\n*F\n+ 1 Nav.kt\nui/NavKt$screen$1$1\n+ 2 Graph.kt\napp/GraphKt\n" +
		"*L\n1#1,104:1\n39#2,2:166\n7#2,3:170,2\n*E\n*S KotlinDebug\n*F\n+ 1 Other.kt\nx/Y\n*L\n1#1:500\n*E\n"
	ranges, origin := smapRanges(smap)
	if origin != "ui/NavKt$screen$1$1" {
		t.Errorf("origin %q", origin)
	}
	c := &Class{Name: "app/GraphKt$g$$inlined$screen$1$1", SmapLines: ranges}
	for line, want := range map[int]string{0: c.Name, 1: "ui/NavKt$screen$1$1", 104: "ui/NavKt$screen$1$1",
		105: c.Name, 166: "app/GraphKt", 167: "app/GraphKt", 168: c.Name, 175: "app/GraphKt", 176: c.Name, 500: c.Name} {
		if got := c.SourceOf(line); got != want {
			t.Errorf("line %d: %q, want %q", line, got, want)
		}
	}
	if r, o := smapRanges("*F\n+ 1 A.kt\n*L\n1#9,x:1\n99999999999#1,99999999999:1,99999999999\n"); o != "" || len(r) > 1 {
		t.Errorf("malformed: %v %q", r, o)
	}
}

// Every call of a fixture method has the line it is on, from the LineNumberTable.
func TestRefLines(t *testing.T) {
	for _, data := range fixtureClasses(t) {
		c, err := Parse(data)
		if err != nil || c.Name != "navapp/sections/SectionsAppKt$onboardingSteps$$inlined$screen$1$1" {
			continue
		}
		var welcome []int
		for _, m := range c.Methods {
			for _, r := range m.Refs {
				if r.Kind == RCall && r.Name == "WelcomeScreen" {
					welcome = append(welcome, r.Line)
				}
			}
		}
		if len(welcome) != 1 || c.SourceOf(welcome[0]) != "navapp/sections/SectionsAppKt" {
			t.Errorf("WelcomeScreen called on lines %v of %v", welcome, c.SmapLines)
		}
		return
	}
	t.Fatal("no copy of the inlined screen wrapper in the fixture")
}

// A forged length anywhere in a class file is an error, never a panic or a giant allocation.
func TestParseForgedLength(t *testing.T) {
	var good []byte
	for _, data := range fixtureClasses(t) {
		if _, err := Parse(data); err == nil && len(data) > 1000 {
			good = data
			break
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	bad := make([]byte, len(good))
	for i := 10; i+4 <= len(good); i++ {
		copy(bad, good)
		binary.BigEndian.PutUint32(bad[i:], 0xFFFFFFF0)
		Parse(bad)
	}
	runtime.ReadMemStats(&after)
	if mb := (after.TotalAlloc - before.TotalAlloc) >> 20; mb > 256 {
		t.Errorf("parsing %d forged copies allocated %d MB", len(good)-13, mb)
	}
}

func TestParamDescsMalformed(t *testing.T) {
	for desc, want := range map[string]int{"(ILjava/lang/String;[J)V": 3, "(ILfoo)V": 1, "([)V": 0, "(I": 0, "": 0} {
		if got := len(ParamDescs(desc)); got != want {
			t.Errorf("%q: %d params, want %d", desc, got, want)
		}
	}
}

// FuzzParse: any bytes parse or fail with an error, in bounded time and memory (a panic Parse
// recovers from is fine; a hang, a stack overflow or running out of memory is not). Run it with a
// memory limit: `(ulimit -v 4000000; go test ./internal/classfile -fuzz FuzzParse -fuzztime 60s
// -fuzzminimizetime 1x)`; without the last flag each new input is minimized for up to a minute,
// shown as 0 execs/sec.
func FuzzParse(f *testing.F) {
	for i, data := range fixtureClasses(f) {
		if i%10 == 0 {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data)
		if err != nil {
			return
		}
		for _, m := range c.Methods {
			ParamDescs(m.Desc)
		}
		for _, fd := range c.Fields {
			_ = ClassOfDesc(fd.Desc)
		}
	})
}

func FuzzParamDescs(f *testing.F) {
	for _, s := range []string{"(ILjava/lang/String;[J)V", "([[Lfoo;Z)V", "(L)V", "("} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, desc string) {
		for _, p := range ParamDescs(desc) {
			if p == "" || len(p) > len(desc) {
				t.Fatalf("%q: param %q", desc, p)
			}
		}
	})
}
