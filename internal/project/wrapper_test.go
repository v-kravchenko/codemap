package project

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWrapper(t *testing.T) {
	var sums strings.Builder
	for i, a := range wrapperArchives {
		fmt.Fprintf(&sums, "%064x  codemap_1.2.3_%s_%s.%s\n", i, a.os, a.arch, a.ext)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.2.3/checksums.txt" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, sums.String())
	}))
	defer srv.Close()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"settings.gradle.kts": `rootProject.name = "x"`})
	// Another release writes its own files: this one runs it by codemapw, as the release does here.
	var ran []string
	defer func(run func(string, []string) int) { runRelease = run }(runRelease)
	runRelease = func(dir string, args []string) int {
		ran = args
		if props, _ := os.ReadFile(filepath.Join(dir, "codemap-wrapper.properties")); !strings.Contains(string(props), "version=1.2.3\n") {
			t.Errorf("codemapw of the release without its properties:\n%s", props)
		}
		if _, err := os.Stat(filepath.Join(root, "codemapw")); err == nil {
			t.Error("files written before the release ran")
		}
		return WrapperCmd(args[1:], "1.2.3")
	}
	if code := WrapperCmd([]string{"--root", root, "--version", "v1.2.3", "--distribution-url", srv.URL + "/v{version}/"}, "dev"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := []string{"wrapper", "--root", root, "--version", "1.2.3", "--distribution-url", srv.URL + "/v{version}/"}; fmt.Sprint(ran) != fmt.Sprint(want) {
		t.Errorf("ran %q, want %q", ran, want)
	}
	props, _ := os.ReadFile(filepath.Join(root, "codemap-wrapper.properties"))
	for _, want := range []string{"version=1.2.3\n", "distribution_url=" + srv.URL + "/v1.2.3\n",
		fmt.Sprintf("sha256_linux_amd64=%064x\n", 2), fmt.Sprintf("sha256_windows_arm64=%064x\n", 5)} {
		if !strings.Contains(string(props), want) {
			t.Errorf("properties without %q:\n%s", want, props)
		}
	}
	if st, err := os.Stat(filepath.Join(root, "codemapw")); err != nil || runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Errorf("codemapw not executable: %v", err)
	}
	if cmd, _ := os.ReadFile(filepath.Join(root, "codemapw.cmd")); !strings.Contains(string(cmd), "\r\n") {
		t.Error("codemapw.cmd without CRLF")
	}
	if script, _ := os.ReadFile(filepath.Join(root, "gradle", "codemap.gradle")); !strings.HasPrefix(string(script), "// codemap 1.2.3:") {
		t.Errorf("gradle/codemap.gradle: %.60s", script)
	}
	if !olderRelease("0.3.9", "0.4.0") || olderRelease("0.10.0", "0.4.0") || olderRelease("0.4.0", "0.4.0") {
		t.Error("olderRelease")
	}
	if _, err := wrapperProperties("1.2.3", srv.URL, map[string]string{}); err == nil {
		t.Error("a release without its archives must fail")
	}
}
