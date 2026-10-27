package query

import "testing"

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
