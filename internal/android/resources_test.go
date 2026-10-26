package android

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const manifest = `<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">
  <application>
    <activity android:name=".MainActivity">
      <intent-filter>
        <action android:name="android.intent.action.MAIN"/>
        <category android:name="android.intent.category.LAUNCHER"/>
      </intent-filter>
    </activity>
    <activity android:name="com.app.LinkActivity">
      <intent-filter>
        <action android:name="android.intent.action.VIEW"/>
        <data android:scheme="https" android:host="app.com" android:pathPrefix="/pay"/>
      </intent-filter>
      <intent-filter>
        <action android:name="android.intent.action.VIEW"/>
        <data android:scheme="app"/>
      </intent-filter>
    </activity>
    <activity android:name="com.app.ShareActivity">
      <intent-filter>
        <action android:name="android.intent.action.SEND"/>
      </intent-filter>
    </activity>
    <activity android:name="com.app.Plain"/>
    <activity-alias android:name=".Alias" android:targetActivity=".MainActivity">
      <intent-filter>
        <action android:name="android.intent.action.MAIN"/>
        <category android:name="android.intent.category.LAUNCHER"/>
      </intent-filter>
    </activity-alias>
  </application>
</manifest>`

func TestParseManifest(t *testing.T) {
	want := []ManifestEntry{
		{".MainActivity", "launcher", "", 4},
		{"com.app.LinkActivity", "deeplink", "https://app.com/pay", 10},
		{"com.app.LinkActivity", "deeplink", "app://*", 10},
		{"com.app.ShareActivity", "intent", "SEND", 20},
		{".MainActivity", "launcher", "", 26},
	}
	if got := ParseManifest(manifest); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if got := ParseManifest("<manifest"); got != nil {
		t.Errorf("broken manifest: %v", got)
	}
}

func TestParseNavigation(t *testing.T) {
	g := ParseNavigation([]byte(`<navigation xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:app="http://schemas.android.com/apk/res-auto" app:startDestination="@id/home">
  <fragment android:id="@+id/home" android:name="com.app.HomeFragment">
    <action android:id="@+id/toDetail" app:destination="@id/detail"/>
    <argument android:name="com.app.NotAScreen"/>
  </fragment>
  <fragment android:id="@+id/detail" android:name="com.app.DetailFragment"/>
  <navigation android:id="@+id/nested" app:startDestination="@id/inner">
    <dialog android:id="@+id/inner" android:name="com.app.InnerDialog"/>
  </navigation>
  <fragment android:id="@+id/noClass"/>
</navigation>`))
	if want := []string{"home", "detail", "inner"}; !slices.Equal(g.DestIDs, want) {
		t.Errorf("dests %v, want %v", g.DestIDs, want)
	}
	if g.Dests["inner"] != "com.app.InnerDialog" {
		t.Errorf("inner: %q", g.Dests["inner"])
	}
	if want := map[string]string{"": "home", "nested": "inner"}; !reflect.DeepEqual(g.Graphs, want) {
		t.Errorf("graphs %v, want %v", g.Graphs, want)
	}
	if want := [][2]string{{"toDetail", "detail"}}; !reflect.DeepEqual(g.Actions, want) {
		t.Errorf("actions %v, want %v", g.Actions, want)
	}
	if g := ParseNavigation([]byte("not xml")); len(g.Dests) != 0 {
		t.Errorf("broken graph: %v", g.Dests)
	}
}

func TestParseXML(t *testing.T) {
	root, err := ParseXML([]byte("\xef\xbb\xbf<a xmlns:x=\"urn:x\" x:k=\"v\">t<b/>tail<!-- c --><c>u</c></a>"))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := root.Get("{urn:x}k"); !ok || v != "v" || len(root.Attrs) != 1 {
		t.Errorf("attrs %v", root.Attrs)
	}
	if root.Text != "t" || len(root.Children) != 2 || root.Children[0].Tail != "tail" || root.Children[1].Text != "u" {
		t.Errorf("text %q, children %d", root.Text, len(root.Children))
	}
	for _, bad := range []string{"", "<a>", "<a></b>", "text only"} {
		if _, err := ParseXML([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestScanResources(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(dir, "res", rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("values/strings.xml", `<resources>
  <string name="title">Hi @string/other</string>
  <style name="Theme.App.Dark"/>
  <style name="Theme.Base" parent="@style/Theme.Root"/>
  <style name="Theme.Sys" parent="android:Theme"/>
  <item name="ref" type="id"/>
</resources>`)
	write("layout/main.xml", `<FrameLayout xmlns:android="http://schemas.android.com/apk/res/android"
    xmlns:tools="http://schemas.android.com/tools" android:background="@drawable/bg" tools:text="@string/ignored">
  <com.app.CustomView android:id="@+id/v" android:text="@android:string/ok"/>
  <fragment android:name="com.app.ListFragment"/>
</FrameLayout>`)
	write("drawable-hdpi/icon.9.png", "")
	byKey := map[string]*Resource{}
	for _, sub := range []string{"values", "layout", "drawable-hdpi"} {
		rtype := ResourceTypeOfDir(sub)
		for _, f := range []string{"strings.xml", "main.xml", "icon.9.png"} {
			p := filepath.Join(dir, "res", sub, f)
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if rtype == "values" {
				for _, r := range parseValues(p, f) {
					byKey[r.Key] = r
				}
			} else {
				r := parseFileResource(p, rtype, f)
				byKey[r.Key] = r
			}
		}
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if want := []string{"drawable/icon", "layout/main", "string/title", "style/Theme_App_Dark", "style/Theme_Base",
		"style/Theme_Sys"}; !slices.Equal(keys, want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	if r := byKey["string/title"]; !r.Refs["string/other"] || r.Line != 2 {
		t.Errorf("title: refs %v, line %d", r.Refs, r.Line)
	}
	if r := byKey["style/Theme_App_Dark"]; !r.Refs["style/Theme_App"] {
		t.Errorf("implicit parent: %v", r.Refs)
	}
	if r := byKey["style/Theme_Base"]; !r.Refs["style/Theme_Root"] {
		t.Errorf("explicit parent: %v", r.Refs)
	}
	if r := byKey["style/Theme_Sys"]; len(r.Refs) != 0 {
		t.Errorf("platform parent: %v", r.Refs)
	}
	main := byKey["layout/main"]
	if !reflect.DeepEqual(main.Refs, map[string]bool{"drawable/bg": true}) {
		t.Errorf("layout refs %v", main.Refs)
	}
	if !main.Classes["com.app.CustomView"] || !main.Classes["com.app.ListFragment"] || len(main.Classes) != 2 {
		t.Errorf("layout classes %v", main.Classes)
	}
}

func TestResourceTypeOfDir(t *testing.T) {
	for dir, want := range map[string]string{"values-uk": "values", "layout-land": "layout", "drawable": "drawable",
		"assets": "", "": ""} {
		if got := ResourceTypeOfDir(dir); got != want {
			t.Errorf("%q: %q, want %q", dir, got, want)
		}
	}
	for base, want := range map[string]string{"a.xml": "a", ".hidden": ".hidden", "..x": "..x", "a.9.png": "a.9", "a": "a"} {
		if got := splitExt(base); got != want {
			t.Errorf("splitExt(%q): %q, want %q", base, got, want)
		}
	}
}

// FuzzParseXML: any bytes parse or fail with an error, and the manifest and navigation readers
// over them neither panic nor hang. Run it with a memory limit, as FuzzParse in classfile.
func FuzzParseXML(f *testing.F) {
	f.Add([]byte(manifest))
	f.Add([]byte(`<navigation><fragment/><action/></navigation>`))
	f.Add([]byte(`<resources><style name="a.b"/></resources>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if root, err := ParseXML(data); err == nil {
			var b strings.Builder
			root.Write(&b)
			scanElement(root, newResource("k", "f", 0))
		}
		ParseManifest(string(data))
		ParseNavigation(data)
	})
}
