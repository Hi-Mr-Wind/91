package catalog

import (
	"context"
	"testing"
	"time"
)

func TestListVideosKeywordMatchesFileName(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	now := time.Now()
	if err := cat.UpsertVideo(ctx, &Video{
		ID:          "p115-115-sone-089-4k",
		DriveID:     "drive",
		FileID:      "file-sone-089-4k",
		FileName:    "www.98T.la@sone-089-4k.mp4",
		Title:       "www.98T.la@sone-089",
		Author:      "4k",
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}

	items, total, err := cat.ListVideos(ctx, ListParams{
		Keyword:  "www.98T.la@sone-089-4k.mp4",
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list videos: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(items) != 1 || items[0].ID != "p115-115-sone-089-4k" {
		t.Fatalf("items = %#v, want seeded video", items)
	}
}

func TestListVideosAdvancedFilters(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})

	seed := func(id, driveID string, createdAt, publishedAt time.Time, durationSeconds int) {
		t.Helper()
		if err := cat.UpsertVideo(ctx, &Video{
			ID:              id,
			DriveID:         driveID,
			FileID:          "file-" + id,
			Title:           id,
			DurationSeconds: durationSeconds,
			PublishedAt:     publishedAt,
			CreatedAt:       createdAt,
			UpdatedAt:       createdAt,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	localDate := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 12, 0, 0, 0, time.Local)
	}
	seed("scriptcrawler-crawler-a-source-1", "cloud-a", localDate(2026, time.July, 10), localDate(2026, time.June, 1), 5*60)
	seed("scriptcrawler-crawler-b-source-2", "cloud-a", localDate(2026, time.July, 15), localDate(2026, time.June, 15), 15*60)
	seed("manual-video", "cloud-b", localDate(2026, time.July, 20), localDate(2026, time.July, 1), 0)
	if err := cat.MarkCrawlerSourceSeen(ctx, "scriptcrawler", "crawler-a", "source-1", "imported", "scriptcrawler-crawler-a-source-1", "", 0); err != nil {
		t.Fatalf("mark crawler-a source: %v", err)
	}
	if err := cat.MarkCrawlerSourceSeen(ctx, "scriptcrawler", "crawler-b", "source-2", "imported", "scriptcrawler-crawler-b-source-2", "", 0); err != nil {
		t.Fatalf("mark crawler-b source: %v", err)
	}
	if err := cat.MarkCrawlerSourceSeen(ctx, "scriptcrawler", "crawler-a", "duplicate", "duplicate", "manual-video", "", 0); err != nil {
		t.Fatalf("mark duplicate crawler source: %v", err)
	}

	assertIDs := func(name string, params ListParams, want ...string) {
		t.Helper()
		params.Page = 1
		params.PageSize = 20
		items, total, err := cat.ListVideos(ctx, params)
		if err != nil {
			t.Fatalf("%s: list videos: %v", name, err)
		}
		if total != len(want) || len(items) != len(want) {
			t.Fatalf("%s: total/items = %d/%d, want %d", name, total, len(items), len(want))
		}
		got := make(map[string]bool, len(items))
		for _, item := range items {
			got[item.ID] = true
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("%s: missing %s in %#v", name, id, got)
			}
		}
	}

	assertIDs("drive", ListParams{DriveID: "cloud-a"},
		"scriptcrawler-crawler-a-source-1", "scriptcrawler-crawler-b-source-2")
	assertIDs("crawler", ListParams{CrawlerID: "crawler-a"},
		"scriptcrawler-crawler-a-source-1")
	assertIDs("created range", ListParams{
		CreatedAtFrom:   time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local).UnixMilli(),
		CreatedAtBefore: time.Date(2026, time.July, 20, 0, 0, 0, 0, time.Local).UnixMilli(),
	}, "scriptcrawler-crawler-b-source-2")
	assertIDs("duration range", ListParams{
		DurationSecondsMin: 10 * 60,
		DurationSecondsMax: 20 * 60,
	}, "scriptcrawler-crawler-b-source-2")
	assertIDs("duration max excludes unknown", ListParams{
		DurationSecondsMax: 20 * 60,
	}, "scriptcrawler-crawler-a-source-1", "scriptcrawler-crawler-b-source-2")
	assertIDs("combined", ListParams{
		DriveID:            "cloud-a",
		CrawlerID:          "crawler-a",
		CreatedAtFrom:      time.Date(2026, time.July, 10, 0, 0, 0, 0, time.Local).UnixMilli(),
		CreatedAtBefore:    time.Date(2026, time.July, 11, 0, 0, 0, 0, time.Local).UnixMilli(),
		DurationSecondsMin: 4 * 60,
		DurationSecondsMax: 6 * 60,
	}, "scriptcrawler-crawler-a-source-1")
}
