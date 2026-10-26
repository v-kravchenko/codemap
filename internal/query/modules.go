package query

// --module: gitignore-style globs over module paths (docs/decisions/0013-module-globs.md).

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/v-kravchenko/codemap/internal/util"
)

// moduleFilter: the globs in order; the last one matching a module decides, a module no glob
// matches is kept only when the first glob is a `!` one (everything but). nil keeps every module.
type moduleFilter struct {
	rules []moduleRule
	base  bool
	kept  map[string]bool
}

type moduleRule struct {
	glob string
	re   *regexp.Regexp
	not  bool
}

// moduleGlobRe: a glob as a regexp over a module path: it takes the module and the ones under it,
// `*` is a run within one segment, `**` any run of segments; `:a:b` (Gradle) is `a/b`.
func moduleGlobRe(glob string) (*regexp.Regexp, bool) {
	p := strings.TrimSpace(glob)
	not := strings.HasPrefix(p, "!")
	p = strings.TrimPrefix(p, "!")
	if strings.HasPrefix(p, ":") {
		p = strings.ReplaceAll(p[1:], ":", "/")
	}
	p = strings.Trim(p, "/")
	if p == "" {
		panic(util.UserError(fmt.Sprintf("argument --module: empty glob %q", glob)))
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch {
		case strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			i++
		case p[i] == '*':
			b.WriteString("[^/]*")
		default:
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
		}
	}
	return regexp.MustCompile("^" + b.String() + "(?:/.*)?$"), not
}

func newModuleFilter(globs []string) *moduleFilter {
	if len(globs) == 0 {
		return nil
	}
	f := &moduleFilter{kept: map[string]bool{}}
	for _, glob := range globs {
		re, not := moduleGlobRe(glob)
		f.rules = append(f.rules, moduleRule{glob, re, not})
	}
	f.base = f.rules[0].not
	return f
}

func (f *moduleFilter) keeps(module string) bool {
	if f == nil {
		return true
	}
	if k, ok := f.kept[module]; ok {
		return k
	}
	k := f.base
	for _, r := range f.rules {
		if r.re.MatchString(module) {
			k = !r.not
		}
	}
	f.kept[module] = k
	return k
}

// moduleFilter: the --module globs over the map's modules; a glob matching none is a typo, not an
// empty answer.
func (g *Graph) moduleFilter(globs []string) *moduleFilter {
	f := newModuleFilter(globs)
	if f == nil {
		return nil
	}
	for _, r := range f.rules {
		found := false
		for i := range g.m.Module.Name.Len() {
			if r.re.MatchString(g.m.Module.Name.At(i)) {
				found = true
				break
			}
		}
		if !found {
			panic(util.UserError(fmt.Sprintf("argument --module: %q matches no module of the code map", r.glob)))
		}
	}
	return f
}

// outsideLine: what --module left out, for the text output.
func outsideLine(parts ...string) string {
	return "Outside --module (not shown): " + strings.Join(parts, ", ")
}
