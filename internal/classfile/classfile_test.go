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

// A forged length anywhere in a class file is an error, never a panic or a giant allocation.
func TestParseForgedLength(t *testing.T) {
	zr, err := zip.OpenReader(filepath.Join(fixture.TestdataDir(), "navapp", "classes.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var good []byte
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".class") {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			good, err = io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(good); err == nil && len(good) > 1000 {
				break
			}
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
