package query

import (
	"strings"
	"testing"

	"github.com/v-kravchenko/codemap/internal/util"
)

// parseErr: the error parseOpts stops with, "" when it takes the arguments.
func parseErr(cmd string, args ...string) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = string(r.(util.UserError))
		}
	}()
	parseOpts(cmd, args)
	return ""
}

// TestCommandsCoverOptions: every option the table gives a query command is one parseOpts takes, so
// the help never offers an option the parser rejects.
func TestCommandsCoverOptions(t *testing.T) {
	values := map[string]string{"direction": "LR", "entry": "deeplink", "kind": "all", "type": "class"}
	for _, c := range commands {
		if !c.query && c.name != "build --info" {
			continue
		}
		cmd := strings.Fields(c.name)[0]
		for _, f := range c.allFlags() {
			args := []string{"--" + f.name}
			if f.arg != "" {
				v := values[f.name]
				if v == "" {
					v = "1"
				}
				args = append(args, v)
			}
			if msg := parseErr(cmd, args...); msg != "" {
				t.Errorf("%s %s: %s", cmd, strings.Join(args, " "), msg)
			}
		}
	}
}

func TestOptionsOfOtherCommands(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{"impact X --ways", "argument --ways: not an option of impact (codemap impact --help)"},
		{"used-by X --in", "argument --in: not an option of used-by (codemap used-by --help)"},
		{"uses X --no-self", "argument --no-self: not an option of uses (codemap uses --help)"},
		{"uses X --no-di", "argument --no-di: not an option of uses (codemap uses --help)"},
		{"find X --type route", "argument --type: invalid choice: 'route' (choose from class, method, field, resource)"},
		{"impact X --unreachable", "argument --unreachable: not an option of impact (codemap impact --help)"},
		{"build --info --force", "argument --force: not an option of build --info (codemap build --help)"},
		{"graph X --bogus", "unrecognized arguments: --bogus"},
		{"graph X Y", "unrecognized arguments: Y"},
		{"changes X", "unrecognized arguments: X"},
		{"impact X Y --nav --kind=all", ""},
		{"nav-path X --via=A --via B --json", ""},
		{"find X --all=true", "argument --all: takes no value, got 'true'"},
		{"impact X --json=", "argument --json: takes no value, got ''"},
		{"impact X --depth=-5", "argument --depth: expected N >= 0"},
		{"impact X --depth=0", ""},
	} {
		args := strings.Fields(tc.args)
		if got := parseErr(args[0], args[1:]...); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.args, got, tc.want)
		}
	}
}
