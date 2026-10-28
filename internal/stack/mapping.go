package stack

// R8/ProGuard mapping.txt: `original.Class -> a.b:`, then its members, a method as
// `[start:end:]type name(args)[:origStart[:origEnd]] -> obf`. Rows of one obfuscated name and line
// range are an inline chain, the inlined body first; a row's name is qualified when the body came
// from another class.

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type methodRow struct {
	obfStart, obfEnd   int // 0, 0: no line range
	class, name        string
	origStart, origEnd int // -1: not given (the lines are the obfuscated ones; one line); 0: unknown
}

type classMap struct {
	original, file string
	methods        map[string][]methodRow // by obfuscated name, in the file's order
}

// Mapping: the classes of a mapping.txt by obfuscated name.
type Mapping struct {
	classes map[string]*classMap
	files   map[string]string // original class -> its source file
}

var (
	classRow  = regexp.MustCompile(`^(\S+) -> (\S+):$`)
	memberRow = regexp.MustCompile(`^(?:(\d+):(\d+):)?\S+ ([^\s(]+)\([^)]*\)((?::\d+){0,2}) -> (\S+)$`)
)

// ReadMapping: the mapping in r; rows it cannot read (fields, metadata) are skipped.
func ReadMapping(r io.Reader) (*Mapping, error) {
	m := &Mapping{classes: map[string]*classMap{}, files: map[string]string{}}
	var cur *classMap
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			cur = nil
			if c := classRow.FindStringSubmatch(line); c != nil {
				cur = &classMap{original: c[1], methods: map[string][]methodRow{}}
				m.classes[c[2]] = cur
			}
			continue
		}
		if cur == nil {
			continue
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") { // a class's metadata, indented or not
			var meta struct{ ID, FileName string }
			if json.Unmarshal([]byte(line[2:]), &meta) == nil && meta.ID == "sourceFile" {
				cur.file = meta.FileName
				m.files[cur.original] = meta.FileName
			}
			continue
		}
		f := memberRow.FindStringSubmatch(line)
		if f == nil {
			continue
		}
		row := methodRow{obfStart: atoi(f[1]), obfEnd: atoi(f[2]), class: cur.original, name: f[3], origStart: -1, origEnd: -1}
		if i := strings.LastIndexByte(f[3], '.'); i >= 0 {
			row.class, row.name = f[3][:i], f[3][i+1:]
		}
		if orig := strings.Split(strings.TrimPrefix(f[4], ":"), ":"); f[4] != "" {
			row.origStart = atoi(orig[0])
			if len(orig) > 1 {
				row.origEnd = atoi(orig[1])
			}
		}
		cur.methods[f[5]] = append(cur.methods[f[5]], row)
	}
	return m, sc.Err()
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// Retrace: the frames the obfuscated f stands for, the inlined body first; f itself when the
// mapping does not rename its class. A method whose rows do not tell one name keeps its
// obfuscated name in the source class.
func (m *Mapping) Retrace(f Frame) []Frame {
	c, ok := m.classes[f.Class]
	if !ok {
		return []Frame{f}
	}
	orig := f
	out := func(class, method string, line int) Frame {
		file := m.files[class]
		if file == "" {
			file = f.File
		}
		return Frame{Class: class, Method: method, File: file, Line: line, Obfuscated: &orig}
	}
	rows := c.methods[f.Method]
	var chain []Frame
	if f.Line > 0 {
		for _, r := range rows {
			if r.obfStart > 0 && r.obfStart <= f.Line && f.Line <= r.obfEnd {
				chain = append(chain, out(r.class, r.name, r.line(f.Line)))
			}
		}
	}
	if len(chain) > 0 {
		return chain
	}
	names := map[[2]string]bool{}
	for _, r := range rows {
		names[[2]string{r.class, r.name}] = true
	}
	if len(names) == 1 {
		for k := range names {
			return []Frame{out(k[0], k[1], 0)}
		}
	}
	return []Frame{out(c.original, f.Method, 0)}
}

// line: the source line of obfuscated line n in the row's range; a range onto one line maps all
// of it there.
func (r methodRow) line(n int) int {
	switch {
	case r.origStart < 0:
		return n
	case r.origEnd > r.origStart:
		return r.origStart + n - r.obfStart
	}
	return r.origStart
}
