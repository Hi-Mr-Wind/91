package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/config"
	"github.com/video-site/backend/internal/drives/localupload"
)

func TestMaintainLocalUploadFileNamesRenamesPhysicalFileAndCatalog(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	previewDir := filepath.Join(root, "previews")
	uploadDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	cat, err := catalog.Open(filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer cat.Close()
	oldName := "upload-1234567890abcdef.mp4"
	if err := os.WriteFile(filepath.Join(uploadDir, oldName), []byte("video"), 0o644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID: "local-video", DriveID: localupload.DriveID, FileID: oldName,
		FileName: "original.mp4", Title: "用户标题", Author: "用户上传", Size: 5,
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}
	app := &App{cat: cat, cfg: &config.Config{Storage: config.Storage{LocalPreviewDir: previewDir}}}
	app.maintainLocalUploadFileNames(ctx)

	got, err := cat.GetVideo(ctx, "local-video")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if got.FileID != "用户标题.mp4" || got.FileName != got.FileID || got.Title != "用户标题" {
		t.Fatalf("identity = fileID %q fileName %q title %q", got.FileID, got.FileName, got.Title)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, got.FileID)); err != nil {
		t.Fatalf("renamed file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(uploadDir, oldName)); !os.IsNotExist(err) {
		t.Fatalf("old file still exists: %v", err)
	}
}

func TestMaintainLocalUploadFileNamesAddsSuffixWithoutOverwrite(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	uploadDir := filepath.Join(root, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	oldName := "upload-1234567890abcdef.mp4"
	if err := os.WriteFile(filepath.Join(uploadDir, oldName), []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "同名.mp4"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write collision: %v", err)
	}
	cat, err := catalog.Open(filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	defer cat.Close()
	now := time.Now()
	if err := cat.UpsertVideo(ctx, &catalog.Video{
		ID: "local-collision", DriveID: localupload.DriveID, FileID: oldName,
		FileName: "old.mp4", Title: "同名", Size: 6,
		PublishedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}
	app := &App{cat: cat, cfg: &config.Config{Storage: config.Storage{LocalPreviewDir: filepath.Join(root, "previews")}}}
	app.maintainLocalUploadFileNames(ctx)
	got, err := cat.GetVideo(ctx, "local-collision")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if !strings.HasPrefix(got.FileID, "同名-") || got.Title != strings.TrimSuffix(got.FileID, ".mp4") {
		t.Fatalf("collision identity = file %q title %q", got.FileID, got.Title)
	}
	existing, err := os.ReadFile(filepath.Join(uploadDir, "同名.mp4"))
	if err != nil || string(existing) != "existing" {
		t.Fatalf("existing file overwritten: data=%q err=%v", existing, err)
	}
}
