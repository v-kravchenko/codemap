package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestVersion: --version and version print the version the build set.
func TestVersion(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		cmd := exec.Command(os.Args[0], arg)
		cmd.Env = append(os.Environ(), "CODEMAP_AS_MAIN=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !strings.HasPrefix(string(out), "codemap ") || strings.TrimSpace(string(out)) == "codemap" {
			t.Errorf("codemap %s: %v %q", arg, err, out)
		}
	}
}
