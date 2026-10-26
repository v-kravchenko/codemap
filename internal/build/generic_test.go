package build

import (
	"strings"
	"testing"
)

func TestGenericSupers(t *testing.T) {
	for sig, want := range map[string]string{
		"Ljava/lang/Object;Lp/S<Lp/A$VM;>;":                    "Lp/S<Lp/A$VM;>;",
		"<VM:Ljava/lang/Object;>Ljava/lang/Object;Lp/S<TVM;>;": "",
		"Lp/B<Lp/X;>;Lp/S<Lp/A;Lp/C<Lp/D;>;>;Lp/R;":            "Lp/B<Lp/X;>; Lp/S<Lp/A;Lp/C<Lp/D;>;>;",
		"Ljava/lang/Object;Lp/S<Lp/A;TK;>;":                    "",
		"Ljava/lang/Object;Lp/S<*>;Lp/S<+Lp/A;>;":              "",
		"Ljava/lang/Object;Lp/S<[Lp/A;>;":                      "Lp/S<[Lp/A;>;",
		"":                                                     "",
	} {
		if got := strings.Join(genericSupers(sig), " "); got != want {
			t.Errorf("genericSupers(%q) = %q, want %q", sig, got, want)
		}
	}
}
