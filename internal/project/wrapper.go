package project

import (
	"bufio"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/v-kravchenko/codemap/internal/util"
)

// The release download of codemapw, with {version} for the release.
const defaultDistributionURL = "https://github.com/v-kravchenko/codemap/releases/download/v{version}"

var releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// The first release with `codemap wrapper`: an older one cannot write its files.
const firstWrapperRelease = "0.4.0"

// Set for the release codemapw runs on behalf of another one: it must write the files itself.
const wrapperDelegateEnv = "CODEMAP_WRAPPER_DELEGATE"

// runRelease runs codemapw of dir with args; a variable for the tests, which have no release.
var runRelease = func(dir string, args []string) int {
	path := filepath.Join(dir, "codemapw")
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), wrapperDelegateEnv+"=1")
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		util.Fail("%v", err)
	}
	return 0
}

// The systems of the release archives (.goreleaser.yaml): codemapw takes the sha256 of its own.
var wrapperArchives = []struct{ os, arch, ext string }{
	{"darwin", "amd64", "tar.gz"}, {"darwin", "arm64", "tar.gz"},
	{"linux", "amd64", "tar.gz"}, {"linux", "arm64", "tar.gz"},
	{"windows", "amd64", "zip"}, {"windows", "arm64", "zip"},
}

//go:embed wrapper/codemapw
var wrapperSh []byte

//go:embed wrapper/codemapw.cmd
var wrapperCmd []byte

// WrapperCmd: codemap wrapper [--version V] [--distribution-url URL]: writes codemapw, codemapw.cmd,
// codemap-wrapper.properties and gradle/codemap.gradle into the project, so a checkout runs one
// release of codemap and its build keeps the model and the map fresh. The only command besides
// init that reaches out: for the checksums of the release.
func WrapperCmd(args []string, toolVersion string) int {
	fs := flag.NewFlagSet("wrapper", flag.ExitOnError)
	root := fs.String("root", util.FindRoot(), "project root")
	version := fs.String("version", toolVersion, "the codemap release (default: this one)")
	url := fs.String("distribution-url", defaultDistributionURL, "where the release archives are, {version} for the release (a mirror)")
	fs.Parse(args)
	abs, _ := filepath.Abs(*root)
	if !hasSettings(abs) {
		util.Fail("No settings.gradle under %s: run from the root of a Gradle build.", abs)
	}
	v := strings.TrimPrefix(*version, "v")
	if !releaseVersion.MatchString(v) {
		util.Fail("%q is not a release (a build from a checkout?): pass --version X.Y.Z.", v)
	}
	if olderRelease(v, firstWrapperRelease) {
		util.Fail("codemap %s has no wrapper: pass --version %s or later.", v, firstWrapperRelease)
	}
	base := strings.TrimSuffix(strings.ReplaceAll(*url, "{version}", v), "/")
	sums, err := fetchChecksums(base + "/checksums.txt")
	if err != nil {
		util.Fail("%v", err)
	}
	props, err := wrapperProperties(v, base, sums)
	if err != nil {
		util.Fail("%v", err)
	}
	if v != strings.TrimPrefix(toolVersion, "v") {
		// The hook and codemapw belong to the release they run: that release writes them, run by
		// codemapw from a temporary directory, so a failed download leaves the project as it was.
		if os.Getenv(wrapperDelegateEnv) != "" {
			util.Fail("codemap %s was run as release %s: a broken release?", toolVersion, v)
		}
		return delegateWrapper(abs, v, *url, props)
	}
	script := append([]byte(fmt.Sprintf("// codemap %s: written by `codemap wrapper`, which rewrites it for another release.\n", v)),
		GradleScript...)
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{"codemapw", wrapperSh, 0o755},
		{"codemapw.cmd", crlf(wrapperCmd), 0o644},
		{"codemap-wrapper.properties", []byte(props), 0o644},
		{filepath.Join("gradle", "codemap.gradle"), script, 0o644},
	}
	for _, f := range files {
		p := filepath.Join(abs, f.path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			util.Fail("%v", err)
		}
		if err := os.WriteFile(p, f.data, f.mode); err != nil {
			util.Fail("%v", err)
		}
		os.Chmod(p, f.mode) // an existing file keeps its mode on WriteFile
	}
	settings := "settings.gradle"
	line := `apply from: 'gradle/codemap.gradle'`
	if util.IsFile(filepath.Join(abs, "settings.gradle.kts")) {
		settings, line = "settings.gradle.kts", `apply(from = "gradle/codemap.gradle")`
	}
	applied := false
	if data, err := os.ReadFile(filepath.Join(abs, settings)); err == nil {
		applied = strings.Contains(string(data), "gradle/codemap.gradle")
	}
	fmt.Fprintf(os.Stderr, "codemap wrapper: codemap %s -> codemapw, codemapw.cmd, codemap-wrapper.properties, gradle/codemap.gradle\n", v)
	if !applied {
		fmt.Fprintf(os.Stderr, "Add to the end of %s, so each build keeps the map fresh:\n  %s\n", settings, line)
	}
	fmt.Fprintln(os.Stderr, "Keep build/codemap and codemap.model.json out of git; commit the four files.")
	return 0
}

// delegateWrapper runs `codemap wrapper` of release v, by this codemapw with v's properties.
func delegateWrapper(root, v, url, props string) int {
	tmp, err := os.MkdirTemp("", "codemapw")
	if err != nil {
		util.Fail("%v", err)
	}
	defer os.RemoveAll(tmp)
	for name, data := range map[string][]byte{"codemapw": wrapperSh, "codemapw.cmd": crlf(wrapperCmd),
		"codemap-wrapper.properties": []byte(props)} {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o755); err != nil {
			util.Fail("%v", err)
		}
	}
	return runRelease(tmp, []string{"wrapper", "--root", root, "--version", v, "--distribution-url", url})
}

// olderRelease: whether release a comes before release b, both X.Y.Z.
func olderRelease(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range pa {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x < y
		}
	}
	return false
}

func crlf(b []byte) []byte { return []byte(strings.ReplaceAll(string(b), "\n", "\r\n")) }

// wrapperProperties: the release, where it is, and the sha256 of each archive from its checksums.
func wrapperProperties(version, base string, sums map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString("# The codemap release ./codemapw runs, checked by sha256; `codemap wrapper` writes this file.\n")
	fmt.Fprintf(&b, "version=%s\ndistribution_url=%s\n", version, base)
	for _, a := range wrapperArchives {
		name := fmt.Sprintf("codemap_%s_%s_%s.%s", version, a.os, a.arch, a.ext)
		sum, ok := sums[name]
		if !ok {
			return "", fmt.Errorf("no %s in the checksums of release %s", name, version)
		}
		fmt.Fprintf(&b, "sha256_%s_%s=%s\n", a.os, a.arch, sum)
	}
	return b.String(), nil
}

// fetchChecksums: the "sha256  file" lines of a release's checksums.txt.
func fetchChecksums(url string) (map[string]string, error) {
	client := &http.Client{Timeout: time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot download %s: %s (no such release?)", url, resp.Status)
	}
	sums := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 {
			sums[f[1]] = f[0]
		}
	}
	return sums, sc.Err()
}
