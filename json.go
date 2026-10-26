package main

// JSON in the bytes of Python's json.dump(indent=1, ensure_ascii=False): objects keep key order.

import (
	"bufio"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type kv struct {
	k string
	v any
}

type obj []kv

func (o obj) get(k string) string {
	for _, x := range o {
		if x.k == k {
			s, _ := x.v.(string)
			return s
		}
	}
	return ""
}

func (o obj) getInt(k string) int {
	for _, x := range o {
		if x.k == k {
			n, _ := x.v.(int)
			return n
		}
	}
	return 0
}

func strs(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func objs(s []obj) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

func list(s []any) []any {
	if s == nil {
		return []any{}
	}
	return s
}

func countsObj(m map[string]int) obj {
	out := obj{}
	for _, k := range sortedKeys(m) {
		out = append(out, kv{k, m[k]})
	}
	return out
}

func sortObjs(s []obj, keys ...string) {
	sort.SliceStable(s, func(i, j int) bool {
		for _, k := range keys {
			if a, b := s[i].get(k), s[j].get(k); a != b {
				return a < b
			}
		}
		return false
	})
}

func writeJSON(b *bufio.Writer, v any, indent int) {
	pad := strings.Repeat(" ", indent+1)
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case float64:
		s := strconv.FormatFloat(x, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		b.WriteString(s)
	case string:
		writeString(b, x)
	case []string:
		writeJSON(b, strs(x), indent)
	case obj:
		if len(x) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, e := range x {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			writeString(b, e.k)
			b.WriteString(": ")
			writeJSON(b, e.v, indent+1)
		}
		b.WriteString("\n" + pad[1:] + "}")
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, e := range x {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			writeJSON(b, e, indent+1)
		}
		b.WriteString("\n" + pad[1:] + "]")
	default:
		panic(fmt.Sprintf("json: %T", v))
	}
}

func writeString(b *bufio.Writer, s string) {
	b.WriteByte('"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		b.WriteString(s[start:i])
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			fmt.Fprintf(b, `\u%04x`, c)
		}
		start = i + 1
	}
	b.WriteString(s[start:])
	b.WriteByte('"')
}
