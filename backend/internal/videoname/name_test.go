package videoname

import "testing"

func TestTitleFromFileNameRemovesOnlyLastExtension(t *testing.T) {
	if got := TitleFromFileName("a.part.MP4"); got != "a.part" {
		t.Fatalf("title = %q, want a.part", got)
	}
}

func TestValidateUploadTitle(t *testing.T) {
	for _, invalid := range []string{"", " bad", "bad.", "bad/name", "bad\x00name"} {
		if err := ValidateUploadTitle(invalid, ".mp4"); err == nil {
			t.Fatalf("ValidateUploadTitle(%q) succeeded", invalid)
		}
	}
	if err := ValidateUploadTitle("正常标题", ".mp4"); err != nil {
		t.Fatalf("valid title: %v", err)
	}
}

func TestUploadFileNameCollisionSuffix(t *testing.T) {
	got := UploadFileName("标题", ".mp4", "upload-1234567890", true)
	if got != "标题-34567890.mp4" {
		t.Fatalf("name = %q", got)
	}
}
