package classfile

import (
	"encoding/binary"
	"errors"
	"strings"
)

const (
	RCall = iota + 1
	RRead
	RWrite
	RType
	RClassLit
	RLambda
	RInline
	RTableRead
	RTableSet
	RString // Owner: a string constant, or the recipe of a string template (\x01 per value)
)

type Arg struct {
	K           byte // 0 none, 'c' static field, 'n' null, 'p' param, 'i' int
	Owner, N, D string
	V           int
}

type Ref struct {
	Kind              uint8
	Owner, Name, Desc string
	Args              []Arg
	Cases             []int32
	Param             int
	Index             int
}

type Guard struct{ End, Param, Bit int }

type Field struct {
	Name, Desc  string
	Signature   string
	Flags       uint16
	Constant    bool
	Annotations []string
}

type Method struct {
	Name, Desc  string
	Flags       uint16
	Annotations []string
	Lines       []int
	Refs        []Ref
	Guards      map[int][]Guard
}

type Class struct {
	Name, Super string
	Interfaces  []string
	Signature   string
	Flags       uint16
	Annotations []string
	Source      string
	Smap        []string
	Enclosing   [3]string
	Fields      []Field
	Methods     []Method
}

type cpe struct {
	tag  byte
	a, b uint16
	s    string
}

var opLen [256]int8

// keepStrings: whether Parse keeps string constants (string routes need them); set by NewBuilder.
var KeepStrings bool

func init() {
	for _, o := range []int{0x10, 0x12, 0x15, 0x16, 0x17, 0x18, 0x19, 0x36, 0x37, 0x38, 0x39, 0x3A, 0xA9, 0xBC} {
		opLen[o] = 1
	}
	for _, o := range []int{0x11, 0x13, 0x14, 0x84, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xBB, 0xBD, 0xC0, 0xC1, 0xC6, 0xC7, 0xA7, 0xA8} {
		opLen[o] = 2
	}
	for o := 0x99; o < 0xA7; o++ {
		opLen[o] = 2
	}
	opLen[0xC5] = 3
	for _, o := range []int{0xB9, 0xBA, 0xC8, 0xC9} {
		opLen[o] = 4
	}
	opLen[0xAA], opLen[0xAB], opLen[0xC4] = -1, -1, -1
}

var errBad = errors.New("bad class file")

type parser struct {
	d      []byte
	cp     []cpe
	concat map[int]string // bootstrap method -> its string template recipe
}

func (p *parser) u16(i int) int        { return int(binary.BigEndian.Uint16(p.d[i:])) }
func (p *parser) u32(i int) int        { return int(binary.BigEndian.Uint32(p.d[i:])) }
func (p *parser) i32(i int) int        { return int(int32(binary.BigEndian.Uint32(p.d[i:]))) }
func (p *parser) utf(k int) string     { return p.cp[k].s }
func (p *parser) classAt(k int) string { return p.utf(int(p.cp[k].a)) }
func (p *parser) memberAt(k int) (string, string, string) {
	e := p.cp[k]
	nt := p.cp[e.b]
	return p.classAt(int(e.a)), p.utf(int(nt.a)), p.utf(int(nt.b))
}

type attr struct {
	name     string
	pos, len int
}

func (p *parser) attributes(q int) (int, []attr) {
	n := p.u16(q)
	q += 2
	out := make([]attr, 0, n)
	for i := 0; i < n; i++ {
		ln := p.u32(q + 2)
		out = append(out, attr{p.utf(p.u16(q)), q + 6, ln})
		q += 6 + ln
	}
	return q, out
}

func (p *parser) skipValue(q int) int {
	tag := p.d[q]
	q++
	switch tag {
	case 'B', 'C', 'D', 'F', 'I', 'J', 'S', 'Z', 's', 'c':
		return q + 2
	case 'e':
		return q + 4
	case '@':
		q, _ = p.skipAnno(q)
		return q
	case '[':
		n := p.u16(q)
		q += 2
		for i := 0; i < n; i++ {
			q = p.skipValue(q)
		}
		return q
	}
	panic(errBad)
}

func (p *parser) skipAnno(q int) (int, string) {
	t := p.utf(p.u16(q))
	n := p.u16(q + 2)
	q += 4
	for i := 0; i < n; i++ {
		q = p.skipValue(q + 2)
	}
	return q, t
}

func ClassOfDesc(d string) string {
	d = strings.TrimLeft(d, "[")
	if len(d) > 2 && d[0] == 'L' && d[len(d)-1] == ';' {
		return d[1 : len(d)-1]
	}
	return ""
}

func (p *parser) annotations(q int) []string {
	n := p.u16(q)
	q += 2
	var out []string
	for i := 0; i < n; i++ {
		var t string
		q, t = p.skipAnno(q)
		if c := ClassOfDesc(t); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func Parse(d []byte) (c *Class, err error) {
	defer func() {
		if r := recover(); r != nil {
			c, err = nil, errBad
		}
	}()
	if len(d) < 10 || binary.BigEndian.Uint32(d) != 0xCAFEBABE {
		return nil, errBad
	}
	p := &parser{d: d}
	count := p.u16(8)
	p.cp = make([]cpe, count)
	q := 10
	for i := 1; i < count; i++ {
		tag := d[q]
		switch tag {
		case 1:
			ln := p.u16(q + 1)
			p.cp[i] = cpe{tag: 1, s: string(d[q+3 : q+3+ln])}
			q += 3 + ln
		case 3, 4:
			q += 5
		case 5, 6:
			q += 9
			i++
		case 7, 8, 16, 19, 20:
			p.cp[i] = cpe{tag: tag, a: uint16(p.u16(q + 1))}
			q += 3
		case 9, 10, 11, 12, 17, 18:
			p.cp[i] = cpe{tag: tag, a: uint16(p.u16(q + 1)), b: uint16(p.u16(q + 3))}
			q += 5
		case 15:
			p.cp[i] = cpe{tag: tag, a: uint16(d[q+1]), b: uint16(p.u16(q + 2))}
			q += 4
		default:
			return nil, errBad
		}
	}
	c = &Class{}
	c.Flags = uint16(p.u16(q))
	c.Name = p.classAt(p.u16(q + 2))
	if s := p.u16(q + 4); s != 0 {
		c.Super = p.classAt(s)
	}
	nIf := p.u16(q + 6)
	q += 8
	for k := 0; k < nIf; k++ {
		c.Interfaces = append(c.Interfaces, p.classAt(p.u16(q+2*k)))
	}
	q += 2 * nIf
	n := p.u16(q)
	q += 2
	for i := 0; i < n; i++ {
		f := Field{Flags: uint16(p.u16(q)), Name: p.utf(p.u16(q + 2)), Desc: p.utf(p.u16(q + 4))}
		var attrs []attr
		q, attrs = p.attributes(q + 6)
		for _, a := range attrs {
			switch a.name {
			case "ConstantValue":
				f.Constant = true
			case "Signature":
				f.Signature = p.utf(p.u16(a.pos))
			case "RuntimeVisibleAnnotations", "RuntimeInvisibleAnnotations":
				f.Annotations = append(f.Annotations, p.annotations(a.pos)...)
			}
		}
		c.Fields = append(c.Fields, f)
	}
	type rawM struct {
		flags      uint16
		name, desc string
		attrs      []attr
	}
	n = p.u16(q)
	q += 2
	raw := make([]rawM, 0, n)
	for i := 0; i < n; i++ {
		m := rawM{flags: uint16(p.u16(q)), name: p.utf(p.u16(q + 2)), desc: p.utf(p.u16(q + 4))}
		q, m.attrs = p.attributes(q + 6)
		raw = append(raw, m)
	}
	_, cattrs := p.attributes(q)
	var bootstrap [][][3]string
	smap := ""
	for _, a := range cattrs {
		switch a.name {
		case "SourceFile":
			c.Source = p.utf(p.u16(a.pos))
		case "Signature":
			c.Signature = p.utf(p.u16(a.pos))
		case "RuntimeVisibleAnnotations", "RuntimeInvisibleAnnotations":
			c.Annotations = append(c.Annotations, p.annotations(a.pos)...)
		case "SourceDebugExtension":
			smap = string(d[a.pos : a.pos+a.len])
		case "EnclosingMethod":
			ci, mi := p.u16(a.pos), p.u16(a.pos+2)
			c.Enclosing[0] = p.classAt(ci)
			if mi != 0 {
				nt := p.cp[mi]
				c.Enclosing[1], c.Enclosing[2] = p.utf(int(nt.a)), p.utf(int(nt.b))
			}
		case "BootstrapMethods":
			b := a.pos + 2
			for k := p.u16(a.pos); k > 0; k-- {
				na := p.u16(b + 2)
				if KeepStrings && na > 0 {
					if _, nm, _ := p.memberAt(int(p.cp[p.u16(b)].b)); nm == "makeConcatWithConstants" {
						if e := p.cp[p.u16(b+4)]; e.tag == 8 {
							if p.concat == nil {
								p.concat = map[int]string{}
							}
							p.concat[len(bootstrap)] = p.utf(int(e.a))
						}
					}
				}
				var hs [][3]string
				for j := 0; j < na; j++ {
					e := p.cp[p.u16(b+4+2*j)]
					if e.tag == 15 {
						t := p.cp[e.b].tag
						if t == 9 || t == 10 || t == 11 {
							o, nm, ds := p.memberAt(int(e.b))
							hs = append(hs, [3]string{o, nm, ds})
						}
					}
				}
				b += 4 + 2*na
				bootstrap = append(bootstrap, hs)
			}
		}
	}
	if smap == "" {
		for _, e := range p.cp {
			if e.tag == 1 && strings.HasPrefix(e.s, "SMAP\n") {
				smap = e.s
				break
			}
		}
	}
	if smap != "" {
		c.Smap = smapClasses(smap)
	}
	c.Methods = make([]Method, len(raw))
	for i, r := range raw {
		m := &c.Methods[i]
		m.Name, m.Desc, m.Flags = r.name, r.desc, r.flags
		for _, a := range r.attrs {
			switch a.name {
			case "RuntimeVisibleAnnotations", "RuntimeInvisibleAnnotations":
				m.Annotations = append(m.Annotations, p.annotations(a.pos)...)
			case "Code":
				p.code(a.pos, bootstrap, m)
			}
		}
	}
	return c, nil
}

func smapClasses(text string) []string {
	lines := strings.Split(text, "\n")
	i := -1
	for k, l := range lines {
		if l == "*F" {
			i = k + 1
			break
		}
	}
	if i < 0 {
		return nil
	}
	var out []string
	first := true
	for i < len(lines) && !strings.HasPrefix(lines[i], "*") {
		if strings.HasPrefix(lines[i], "+ ") {
			if i+1 < len(lines) && !strings.HasPrefix(lines[i+1], "*") && !strings.HasPrefix(lines[i+1], "+ ") {
				if !first {
					out = append(out, strings.TrimSpace(lines[i+1]))
				}
				first = false
				i += 2
				continue
			}
			first = false
		}
		i++
	}
	return out
}

func ParamDescs(desc string) []string {
	var out []string
	end := strings.IndexByte(desc, ')')
	for i := 1; i < end; {
		j := i
		for desc[j] == '[' {
			j++
		}
		if desc[j] == 'L' {
			j += strings.IndexByte(desc[j:], ';')
		}
		out = append(out, desc[i:j+1])
		i = j + 1
	}
	return out
}

type pushed struct {
	k       byte // 0 not a plain push, 'p','c','n','i','u','s'(swap)
	v       int
	o, n, d string
}

type recentV struct {
	op      int
	kind    byte // 'i' int, 's' name, 'f' field
	v       int
	o, n, d string
}

func (p *parser) intPush(c, op int) (int, bool) {
	switch {
	case op >= 0x02 && op <= 0x08:
		return op - 3, true
	case op == 0x10:
		return int(int8(p.d[c+1])), true
	case op == 0x11:
		return int(int16(p.u16(c + 1))), true
	}
	return 0, false
}

func (p *parser) code(pos int, bootstrap [][][3]string, m *Method) {
	d := p.d
	codeLen := p.u32(pos + 4)
	c0 := pos + 8
	end := c0 + codeLen
	slots := map[int]int{}
	slot := 0
	if m.Flags&0x8 == 0 {
		slot = 1
	}
	pds := ParamDescs(m.Desc)
	for i, pd := range pds {
		slots[slot] = i
		if pd == "J" || pd == "D" {
			slot += 2
		} else {
			slot++
		}
	}
	targets := make([]bool, codeLen+1)
	mark := func(t int) {
		if t >= c0 && t <= end {
			targets[t-c0] = true
		}
	}
	for c := c0; c < end; {
		op := int(d[c])
		n := int(opLen[op])
		if (op >= 0x99 && op <= 0xA8) || op == 0xC6 || op == 0xC7 {
			mark(c + int(int16(p.u16(c+1))))
		} else if op == 0xC8 || op == 0xC9 {
			mark(c + p.i32(c+1))
		}
		if n >= 0 {
			c += 1 + n
		} else if op == 0xC4 {
			if d[c+1] == 0x84 {
				c += 6
			} else {
				c += 4
			}
		} else {
			b := c + 1 + (4-((c+1-c0)%4))%4
			mark(c + p.i32(b))
			if op == 0xAA {
				lo, hi := p.i32(b+4), p.i32(b+8)
				for k := 0; k < hi-lo+1; k++ {
					mark(c + p.i32(b+12+4*k))
				}
				c = b + 12 + 4*(hi-lo+1)
			} else {
				pairs := p.i32(b + 4)
				for k := 0; k < pairs; k++ {
					mark(c + p.i32(b+8+8*k+4))
				}
				c = b + 8 + 8*pairs
			}
		}
	}
	for k, cnt := 0, p.u16(end); k < cnt; k++ {
		mark(c0 + p.u16(end+2+8*k+4))
	}
	var recent []recentV
	var window []pushed
	var prev []int
	var guards []Guard
	defaults := strings.HasSuffix(m.Name, "$default")
	table, ordinal := "", ""
	ordParam := -1
	for c := c0; c < end; {
		op := int(d[c])
		cur := c
		first := len(m.Refs)
		if len(guards) > 0 {
			g2 := guards[:0]
			for _, g := range guards {
				if c < g.End {
					g2 = append(g2, g)
				}
			}
			guards = g2
		}
		if targets[c-c0] {
			window = window[:0]
		}
		rv := recentV{op: op}
		if v, ok := p.intPush(c, op); ok {
			rv.kind, rv.v = 'i', v
		}
		if op == 0x3A || (op >= 0x4B && op <= 0x4E) {
			s := op - 0x4B
			if op == 0x3A {
				s = int(d[c+1])
			}
			if len(window) > 0 && window[len(window)-1].k == 'p' {
				if pi, ok := slots[window[len(window)-1].v]; ok {
					slots[s] = pi
				} else {
					delete(slots, s)
				}
			} else {
				delete(slots, s)
			}
		}
		switch {
		case op >= 0xB2 && op <= 0xB9:
			o, nm, ds := p.memberAt(p.u16(c + 1))
			if strings.HasPrefix(o, "[") {
				break
			}
			if op >= 0xB6 {
				m.Refs = append(m.Refs, Ref{Kind: RCall, Owner: o, Name: nm, Desc: ds, Args: callArgs(window, ds, slots)})
				rv.kind, rv.n = 's', nm
				if op == 0xB6 && nm == "ordinal" {
					ordinal = o
					var last *pushed
					if len(window) >= 3 && window[len(window)-1].k == 's' {
						last = &window[len(window)-3]
					} else if len(window) > 0 {
						last = &window[len(window)-1]
					}
					ordParam = -1
					if last != nil && last.k == 'p' {
						if pi, ok := slots[last.v]; ok {
							ordParam = pi
						}
					}
				}
			} else if op == 0xB2 || op == 0xB4 {
				m.Refs = append(m.Refs, Ref{Kind: RRead, Owner: o, Name: nm, Desc: ds})
				rv.kind, rv.o, rv.n, rv.d = 'f', o, nm, ds
				if op == 0xB2 && strings.HasPrefix(nm, "$EnumSwitchMapping$") {
					table = o
				}
			} else {
				m.Refs = append(m.Refs, Ref{Kind: RWrite, Owner: o, Name: nm, Desc: ds})
			}
		case op == 0x4F && len(recent) >= 4:
			r := recent[len(recent)-4:]
			if (r[0].op == 0x19 || (r[0].op >= 0x2A && r[0].op <= 0x2D) || r[0].op == 0xB2) && r[1].op == 0xB2 && r[1].kind == 'f' &&
				r[2].op == 0xB6 && r[2].n == "ordinal" && r[3].kind == 'i' {
				m.Refs = append(m.Refs, Ref{Kind: RTableSet, Owner: r[1].o, Name: r[1].n, Desc: r[1].d, Index: r[3].v})
			}
		case op == 0xBA:
			bi := int(p.cp[p.u16(c+1)].a)
			for _, h := range bootstrap[bi] {
				m.Refs = append(m.Refs, Ref{Kind: RLambda, Owner: h[0], Name: h[1], Desc: h[2]})
			}
			if r, ok := p.concat[bi]; ok {
				m.Refs = append(m.Refs, Ref{Kind: RString, Owner: r})
			}
		case op == 0xBB || op == 0xBD || op == 0xC0 || op == 0xC1 || op == 0xC5:
			nm := p.classAt(p.u16(c + 1))
			if strings.HasPrefix(nm, "[") {
				nm = ClassOfDesc(nm)
			}
			if nm != "" {
				m.Refs = append(m.Refs, Ref{Kind: RType, Owner: nm, Index: op})
			}
		case op == 0x12 || op == 0x13:
			k := int(d[c+1])
			if op == 0x13 {
				k = p.u16(c + 1)
			}
			if p.cp[k].tag == 7 {
				nm := p.classAt(k)
				if strings.HasPrefix(nm, "[") {
					nm = ClassOfDesc(nm)
				}
				if nm != "" {
					m.Refs = append(m.Refs, Ref{Kind: RClassLit, Owner: nm})
				}
			} else if p.cp[k].tag == 8 && KeepStrings {
				m.Refs = append(m.Refs, Ref{Kind: RString, Owner: p.utf(int(p.cp[k].a))})
			}
		}
		n := int(opLen[op])
		if n >= 0 {
			c += 1 + n
		} else if op == 0xC4 {
			if d[c+1] == 0x84 {
				c += 6
			} else {
				c += 4
			}
		} else {
			b := c + 1 + (4-((c+1-c0)%4))%4
			def := p.i32(b)
			var cases []int32
			if op == 0xAA {
				lo, hi := p.i32(b+4), p.i32(b+8)
				for k := 0; k < hi-lo+1; k++ {
					if p.i32(b+12+4*k) != def {
						cases = append(cases, int32(lo+k))
					}
				}
				c = b + 12 + 4*(hi-lo+1)
			} else {
				pairs := p.i32(b + 4)
				for k := 0; k < pairs; k++ {
					if p.i32(b+8+8*k+4) != def {
						cases = append(cases, int32(p.i32(b+8+8*k)))
					}
				}
				c = b + 8 + 8*pairs
			}
			if table != "" && ordinal != "" {
				m.Refs = append(m.Refs, Ref{Kind: RTableRead, Owner: table, Name: ordinal, Cases: cases, Param: ordParam})
				table, ordinal, ordParam = "", "", -1
			}
		}
		if defaults && op == 0x99 && len(prev) >= 3 && d[prev[len(prev)-1]] == 0x7E {
			p3 := prev[len(prev)-3]
			s := -1
			if d[p3] == 0x15 {
				s = int(d[p3+1])
			} else if d[p3] >= 0x1A && d[p3] <= 0x1D {
				s = int(d[p3]) - 0x1A
			}
			bit, ok := p.intPush(prev[len(prev)-2], int(d[prev[len(prev)-2]]))
			if pi, in := slots[s]; in && ok && bit != 0 {
				guards = append(guards, Guard{cur + int(int16(p.u16(cur+1))), pi, bit})
			}
		}
		if len(guards) > 0 {
			if m.Guards == nil {
				m.Guards = map[int][]Guard{}
			}
			for i := first; i < len(m.Refs); i++ {
				m.Guards[i] = append([]Guard(nil), guards...)
			}
		}
		recent = append(recent, rv)
		if len(recent) > 4 {
			recent = recent[1:]
		}
		window = append(window, p.pushedAt(cur, op))
		if len(window) > 16 {
			window = window[1:]
		}
		prev = append(prev, cur)
		if len(prev) > 3 {
			prev = prev[1:]
		}
	}
	q := end
	q += 2 + 8*p.u16(q)
	_, attrs := p.attributes(q)
	for _, a := range attrs {
		switch a.name {
		case "LineNumberTable":
			for k, cnt := 0, p.u16(a.pos); k < cnt; k++ {
				m.Lines = append(m.Lines, p.u16(a.pos+2+4*k+2))
			}
		case "LocalVariableTable":
			seen := map[string]bool{}
			for k, cnt := 0, p.u16(a.pos); k < cnt; k++ {
				nm := p.utf(p.u16(a.pos + 2 + 10*k + 4))
				if fn, ok := strings.CutPrefix(nm, "$i$f$"); ok && !seen[fn] {
					seen[fn] = true
					m.Refs = append(m.Refs, Ref{Kind: RInline, Name: fn})
				}
			}
		}
	}
}

func (p *parser) pushedAt(c, op int) pushed {
	switch {
	case op == 0x19:
		return pushed{k: 'p', v: int(p.d[c+1])}
	case op >= 0x2A && op <= 0x2D:
		return pushed{k: 'p', v: op - 0x2A}
	case op == 0xB2:
		o, n, d := p.memberAt(p.u16(c + 1))
		if strings.HasPrefix(d, "L") {
			return pushed{k: 'c', o: o, n: n, d: d}
		}
		return pushed{k: 'u'}
	case op == 0x01:
		return pushed{k: 'n'}
	case op == 0x5F:
		return pushed{k: 's'}
	}
	if v, ok := p.intPush(c, op); ok {
		return pushed{k: 'i', v: v}
	}
	if (op >= 0x02 && op <= 0x14) || (op >= 0x15 && op <= 0x18) || (op >= 0x1A && op <= 0x29) {
		return pushed{k: 'u'}
	}
	return pushed{}
}

func callArgs(window []pushed, desc string, slots map[int]int) []Arg {
	n := len(ParamDescs(desc))
	var args []Arg
	for j := 1; j <= n; j++ {
		if j > len(window) {
			break
		}
		v := window[len(window)-j]
		if v.k == 0 || v.k == 's' {
			break
		}
		var a Arg
		switch v.k {
		case 'p':
			if pi, ok := slots[v.v]; ok {
				a = Arg{K: 'p', V: pi}
			}
		case 'c':
			a = Arg{K: 'c', Owner: v.o, N: v.n, D: v.d}
		case 'n':
			a = Arg{K: 'n'}
		case 'i':
			a = Arg{K: 'i', V: v.v}
		}
		if a.K != 0 {
			if args == nil {
				args = make([]Arg, n)
			}
			args[n-j] = a
		}
	}
	return args
}
