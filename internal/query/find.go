package query

// find: symbols by name.

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// likeMatch: SQLite LIKE (ASCII case-insensitive, % any run, _ any one byte) for a pattern that
// starts and ends with %: its segments in order, each at its leftmost place.
func likeMatch(segs []string, s string) bool {
	pos := 0
	for _, seg := range segs {
		at := -1
		for i := pos; i+len(seg) <= len(s); i++ {
			if foldHas(s[i:], seg) {
				at = i
				break
			}
		}
		if at < 0 {
			return false
		}
		pos = at + len(seg)
	}
	return true
}

func foldHas(s, seg string) bool {
	for j := 0; j < len(seg); j++ {
		c, p := s[j], seg[j]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if p != '_' && c != p {
			return false
		}
	}
	return true
}

func (g *Graph) cmdFind(o opts) int {
	pattern := o.pos[0]
	rx, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		rx = regexp.MustCompile("(?i)" + regexp.QuoteMeta(pattern))
	}
	var like []string
	for _, seg := range strings.Split(strings.ToLower(regexp.MustCompile(`[^\w]`).ReplaceAllString(pattern, "%")), "%") {
		if seg != "" {
			like = append(like, seg)
		}
	}
	type row struct{ kind, q, module, loc string }
	mf, outside := g.moduleFilter(o.modules), 0
	seen := map[row]bool{}
	var out []row
	synthetic := map[string]bool{}
	for i := range g.m.Nodes() {
		n := int32(i)
		k := g.kind(n)
		if k == "ROUTE" || !(likeMatch(like, g.sname(n)) || likeMatch(like, g.name(n))) {
			continue
		}
		name := g.name(n)
		if k == "M" && (strings.HasPrefix(name, "access$") || strings.HasPrefix(name, "<") || strings.Contains(name, "$lambda")) {
			continue
		}
		q := g.qualified(n)
		if !(rx.MatchString(q) || rx.MatchString(g.sname(n))) {
			continue
		}
		if len(o.types) > 0 && !slices.Contains(o.types, kindName(k)) {
			continue
		}
		cls := n
		if k != "C" {
			cls = g.owner(n)
		}
		if !o.all && cls >= 0 && (isDIGlue(g.name(cls)) || syntheticClass.MatchString(g.name(cls))) {
			synthetic[q] = true
			continue
		}
		r := row{k, q, g.module(n), g.location(n)}
		if seen[r] {
			continue
		}
		seen[r] = true
		if mf.keeps(r.module) {
			out = append(out, r)
		} else {
			outside++
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if la, lb := utf8.RuneCountInString(a.q), utf8.RuneCountInString(b.q); la != lb {
			return la < lb
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.q != b.q {
			return a.q < b.q
		}
		if a.module != b.module {
			return a.module < b.module
		}
		return a.loc < b.loc
	})
	shownRows := out
	if o.limit >= 0 && o.limit < len(out) {
		shownRows = out[:o.limit]
	}
	if o.json {
		syms := make([]any, len(shownRows))
		for i, r := range shownRows {
			syms[i] = obj{{"symbol", r.q}, {"kind", kindName(r.kind)}, {"module", r.module}, {"location", r.loc}}
		}
		result := obj{{"pattern", pattern}, {"total", len(out)}, {"synthetic", len(synthetic)}, {"symbols", syms}}
		if mf != nil {
			result = append(result, kv{"hidden_by_module", obj{{"symbols", outside}}})
		}
		g.printJSON("find", result)
		return exitOK
	}
	for _, r := range shownRows {
		fmt.Printf("%-5s %-70s %-28s %s\n", r.kind, r.q, r.module, r.loc)
	}
	if len(shownRows) < len(out) {
		fmt.Printf("... %d more (--limit)\n", len(out)-len(shownRows))
	}
	if len(synthetic) > 0 {
		fmt.Printf("(%d synthetic hidden: Hilt glue, lambdas, DefaultImpls; --all shows them)\n", len(synthetic))
	}
	if outside > 0 {
		fmt.Printf("(%s)\n", outsideLine(fmt.Sprintf("%d symbols", outside)))
	}
	return exitOK
}
