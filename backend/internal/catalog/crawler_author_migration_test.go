package catalog

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestClearSyntheticCrawlerAuthorsOnce(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	if err := cat.UpsertDrive(ctx, &Drive{
		ID:   "crawler-demo",
		Kind: "scriptcrawler",
		Name: "DemoCrawler",
	}); err != nil {
		t.Fatalf("upsert crawler drive: %v", err)
	}
	now := time.Now()
	seed := func(id, driveID, author string) {
		t.Helper()
		if err := cat.UpsertVideo(ctx, &Video{
			ID:          id,
			DriveID:     driveID,
			FileID:      id + ".mp4",
			FileName:    id + ".mp4",
			Title:       id,
			Author:      author,
			Size:        1,
			PublishedAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			t.Fatalf("upsert video %s: %v", id, err)
		}
	}

	seed("scriptcrawler-crawler-demo-local", "crawler-demo", "crawler-demo")
	seed("scriptcrawler-crawler-demo-migrated", "target-drive", "democrawler")
	seed("scriptcrawler-crawler-demo-real", "target-drive", "Real Publisher")
	seed("local-unrelated", "target-drive", "DemoCrawler")

	if err := cat.SetSetting(ctx, settingCrawlerSyntheticAuthorsCleaned, ""); err != nil {
		t.Fatalf("reset migration marker: %v", err)
	}
	removed, err := cat.clearSyntheticCrawlerAuthorsOnce(ctx)
	if err != nil {
		t.Fatalf("clear synthetic authors: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}

	assertAuthor := func(id, want string) {
		t.Helper()
		video, err := cat.GetVideo(ctx, id)
		if err != nil {
			t.Fatalf("get video %s: %v", id, err)
		}
		if video.Author != want {
			t.Fatalf("video %s author = %q, want %q", id, video.Author, want)
		}
	}
	assertAuthor("scriptcrawler-crawler-demo-local", "")
	assertAuthor("scriptcrawler-crawler-demo-migrated", "")
	assertAuthor("scriptcrawler-crawler-demo-real", "Real Publisher")
	assertAuthor("local-unrelated", "DemoCrawler")

	seed("scriptcrawler-crawler-demo-after-marker", "target-drive", "crawler-demo")
	removed, err = cat.clearSyntheticCrawlerAuthorsOnce(ctx)
	if err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	if removed != 0 {
		t.Fatalf("second removed = %d, want 0", removed)
	}
	assertAuthor("scriptcrawler-crawler-demo-after-marker", "crawler-demo")
}
