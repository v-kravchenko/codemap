package query

import (
	"strings"
	"testing"
)

func TestModuleFilter(t *testing.T) {
	modules := []string{"app", "feature/userdoc", "feature/userdoc/financial", "feature/userdocx",
		"feature/cards/api", "feature/cards/impl", "feature/debug", "core/network"}
	for _, tc := range []struct{ globs, want string }{
		{"feature/userdoc", "feature/userdoc feature/userdoc/financial"},
		{":feature:userdoc", "feature/userdoc feature/userdoc/financial"},
		{"feature/userdoc/", "feature/userdoc feature/userdoc/financial"},
		{"feature/userdoc !feature/userdoc/*", "feature/userdoc"},
		{"feature/userdoc/*", "feature/userdoc/financial"},
		{"feature/userdoc*", "feature/userdoc feature/userdoc/financial feature/userdocx"},
		{"feature/*/impl", "feature/cards/impl"},
		{"**/impl", "feature/cards/impl"},
		{"feature/** !feature/debug", "feature/userdoc feature/userdoc/financial feature/userdocx feature/cards/api feature/cards/impl"},
		{"!feature", "app core/network"},
		{"!feature feature/cards", "app feature/cards/api feature/cards/impl core/network"},
		{"feature !feature/cards feature/cards/api", "feature/userdoc feature/userdoc/financial feature/userdocx feature/cards/api feature/debug"},
		{"app", "app"},
	} {
		f := newModuleFilter(strings.Fields(tc.globs))
		var got []string
		for _, m := range modules {
			if f.keeps(m) {
				got = append(got, m)
			}
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("--module %s: %q, want %q", tc.globs, strings.Join(got, " "), tc.want)
		}
	}
}
