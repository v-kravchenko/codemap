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

// TestMemberName: the names a base map is matched by when a member's id is not in it.
func TestMemberName(t *testing.T) {
	for sname, want := range map[string]string{"open": "open", "getTitle": "title", "setTitle": "title",
		"isPinEnabled": "pinEnabled", "issue": "issue", "get": "get", "<init>": "<init>",
		"open$lambda$0": "open", "notificationManager_delegate$lambda$0": "notificationManager",
		"notificationManager$delegate": "notificationManager", "access$getTitle$p": "title",
		"load$default": "load", "count$app_release": "count"} {
		if got := memberName(sname); got != want {
			t.Errorf("memberName(%s) = %s, want %s", sname, got, want)
		}
	}
	for name, want := range map[string]string{"a/ComposableSingletons$ScreenKt": "a/ScreenKt", "a/Screen": "a/Screen"} {
		if got := sourceClass(name); got != want {
			t.Errorf("sourceClass(%s) = %s, want %s", name, got, want)
		}
	}
}
