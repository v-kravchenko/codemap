package query

import (
	"slices"
	"testing"
)

func TestWordLines(t *testing.T) {
	for _, c := range []struct {
		text string
		want []int32
	}{
		{"KEY", []int32{1}},
		{"a KEY b KEY\nKEY_X\nX_KEY KEY2\n\nfoo(KEY)", []int32{1, 5}},
		{"KEYKEY\n_KEY\nкKEY", nil},
		{"\n\nval x = KEY", []int32{3}},
	} {
		if got := wordLines(c.text, "KEY"); !slices.Equal(got, c.want) {
			t.Errorf("wordLines(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
