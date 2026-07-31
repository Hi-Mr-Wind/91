package scanner

import "testing"

func TestParseStripsBracketPrefixAndAuthor(t *testing.T) {
	got := Parse("[乱七八糟] 女大人妻后入 - 某作者.mp4")
	if got.Title != "女大人妻后入" {
		t.Fatalf("title = %q, want 女大人妻后入", got.Title)
	}
	if got.Author != "某作者" {
		t.Fatalf("author = %q, want 某作者", got.Author)
	}
}

func TestParseDegradesGracefully(t *testing.T) {
	got := Parse("[sunny,kenny] 普通标题.mp4")
	if got.Title != "普通标题" {
		t.Fatalf("title = %q, want 普通标题", got.Title)
	}
	if got.Author != "" {
		t.Fatalf("author = %q, want empty", got.Author)
	}

	plain := Parse("纯标题.mp4")
	if plain.Title != "纯标题" || plain.Author != "" {
		t.Fatalf("plain = %#v", plain)
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
