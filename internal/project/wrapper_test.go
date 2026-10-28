package project

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// codemapw on a download that fails: the marker, the hook's skip for 15 minutes, the manual run that
// tries again and clears the marker. A closed port fails at once; a stalled network is the timeouts' case.
func TestCodemapwDownload(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" || runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("codemapw runs on Linux and macOS")
	}
	for _, tool := range []string{"sh", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("needs %s", tool)
		}
	}
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	script := []byte("#!/bin/sh\necho ran \"$@\"\n")
	if err := tw.WriteHeader(&tar.Header{Name: "codemap", Mode: 0o755, Size: int64(len(script))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(script); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	archive := fmt.Sprintf("codemap_1.2.3_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+archive {
			http.NotFound(w, r)
			return
		}
		w.Write(body.Bytes())
	}))
	defer up.Close()
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	dir, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codemapw"), wrapperSh, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(body.Bytes()))
	writeProps := func(url string) {
		props := fmt.Sprintf("version=1.2.3\ndistribution_url=%s\nsha256_%s_%s=%s\n", url, runtime.GOOS, runtime.GOARCH, sum)
		if err := os.WriteFile(filepath.Join(dir, "codemap-wrapper.properties"), []byte(props), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(hook bool) (stdout, stderr string, failed bool) {
		cmd := exec.Command("sh", filepath.Join(dir, "codemapw"), "build")
		cmd.Env = []string{"CODEMAP_HOME=" + home, "PATH=" + os.Getenv("PATH")}
		if hook {
			cmd.Env = append(cmd.Env, "CODEMAP_HOOK=1")
		}
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		return out.String(), errOut.String(), err != nil
	}
	marker := filepath.Join(home, "1.2.3", ".download-failed")

	writeProps(down.URL)
	if _, errOut, failed := run(true); !failed || !strings.Contains(errOut, "cannot download") {
		t.Fatalf("hook on a closed port: failed %v, %s", failed, errOut)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("no marker after a failed download: %v", err)
	}
	if _, errOut, failed := run(true); !failed || strings.Contains(errOut, "downloading") || !strings.Contains(errOut, "does not try again") {
		t.Errorf("hook within 15 minutes must skip the download: failed %v, %s", failed, errOut)
	}
	if err := os.Chtimes(marker, time.Now().Add(-16*time.Minute), time.Now().Add(-16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, errOut, failed := run(true); !failed || !strings.Contains(errOut, "downloading") {
		t.Errorf("hook after 15 minutes must try again: failed %v, %s", failed, errOut)
	}
	if _, errOut, failed := run(false); !failed || !strings.Contains(errOut, "downloading") {
		t.Errorf("a manual run must try even within 15 minutes: failed %v, %s", failed, errOut)
	}

	writeProps(up.URL)
	if out, errOut, failed := run(false); failed || !strings.Contains(out, "ran build") {
		t.Fatalf("manual run with the server up: failed %v, %s", failed, errOut)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a download that succeeded must clear the marker")
	}
}
