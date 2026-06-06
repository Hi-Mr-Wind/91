package localupload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamURLReturnsStoredUploadPath(t *testing.T) {
	uploadDir := filepath.Join(t.TempDir(), "uploads")
	drv := New(uploadDir)
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	path := filepath.Join(uploadDir, "upload-1.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatalf("write upload: %v", err)
	}

	link, err := drv.StreamURL(context.Background(), "upload-1.mp4")

	if err != nil {
		t.Fatalf("stream url: %v", err)
	}
	if link.URL != path {
		t.Fatalf("url = %q, want %q", link.URL, path)
	}
}

func TestStreamURLRejectsPathTraversal(t *testing.T) {
	drv := New(t.TempDir())

	_, err := drv.StreamURL(context.Background(), "../secret.mp4")

	if err == nil || !strings.Contains(err.Error(), "invalid upload file id") {
		t.Fatalf("error = %v, want invalid upload file id", err)
	}
}
