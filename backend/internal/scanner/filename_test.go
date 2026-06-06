package scanner

import "testing"

func TestParseMatchesOnlyFixedTagsFromFilename(t *testing.T) {
	got := Parse("[乱七八糟] 女大人妻后入口交奶子臀.mp4")
	want := []string{"后入", "奶子", "口交", "臀", "人妻", "女大"}

	if !sameStrings(got.Tags, want) {
		t.Fatalf("tags = %#v, want %#v", got.Tags, want)
	}
}

func TestParseDoesNotKeepBracketTags(t *testing.T) {
	got := Parse("[sunny,kenny] 普通标题.mp4")

	if len(got.Tags) != 0 {
		t.Fatalf("tags = %#v, want none", got.Tags)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
