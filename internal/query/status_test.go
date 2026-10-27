package query

import "testing"

func TestDeclared(t *testing.T) {
	kt := "@Preview\n@Composable\nprivate fun Preview() {\n    Screen(onClick = {})\n}\n" +
		"fun <T> List<T>.second(): T = this[1]\nprivate val title: String = \"\"\ndata class Item(val id: Int)\n" +
		"object Session\nenum class Mode { A }\n"
	for _, c := range []struct {
		name  string
		class bool
		want  bool
	}{{"Preview", false, true}, {"second", false, true}, {"title", false, true}, {"Item", true, true},
		{"Session", true, true}, {"Mode", true, true}, {"Screen", false, false}, {"id", false, true},
		{"Other", true, false}, {"onClick", false, false}} {
		if got := declared(kt, []string{c.name}, c.class, false); got != c.want {
			t.Errorf("kotlin %s: got %v", c.name, got)
		}
	}
	java := "public class Main extends Activity {\n  private int count = 0;\n  void open(Intent i) { start(i); }\n}\n"
	for name, want := range map[string]bool{"count": true, "open": true, "start": false} {
		if got := declared(java, []string{name}, false, true); got != want {
			t.Errorf("java %s: got %v", name, got)
		}
	}
	if !declared(java, []string{"Main"}, true, true) {
		t.Error("java class Main")
	}
}
