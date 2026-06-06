package fixedtags

import "testing"

func TestMatchFilenameMapsSimilarTermsToFixedLabels(t *testing.T) {
	got := MatchFilename("back-shot oral-sex big boobs big ass wife college student.mp4")
	want := []string{"后入", "奶子", "口交", "臀", "人妻", "女大"}

	if !sameStrings(got, want) {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}
}

func TestMatchFilenameMapsChineseSimilarTermsToFixedLabels(t *testing.T) {
	got := MatchFilename("背后式揉乳口活蜜桃臀少妇大学.mp4")
	want := []string{"后入", "奶子", "口交", "臀", "人妻", "女大"}

	if !sameStrings(got, want) {
		t.Fatalf("tags = %#v, want %#v", got, want)
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
