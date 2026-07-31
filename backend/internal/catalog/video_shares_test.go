package catalog

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestVideoShareCanOnlyBeClaimedByOneSession(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	seedShareTestVideo(t, cat, "video-1")

	now := time.Now().Truncate(time.Millisecond)
	if err := cat.CreateVideoShare(ctx, "share-1", "token-hash-1", "video-1", now); err != nil {
		t.Fatalf("create share: %v", err)
	}

	type result struct {
		share *VideoShare
		fresh bool
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, sessionHash := range []string{"session-a", "session-b"} {
		wg.Add(1)
		go func(sessionHash string) {
			defer wg.Done()
			<-start
			share, fresh, err := cat.ClaimVideoShare(
				ctx,
				"token-hash-1",
				sessionHash,
				now,
				now.Add(24*time.Hour),
			)
			results <- result{share: share, fresh: fresh, err: err}
		}(sessionHash)
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	losers := 0
	for got := range results {
		switch {
		case got.err == nil && got.fresh && got.share != nil:
			winners++
		case errors.Is(got.err, ErrVideoShareConsumed):
			losers++
		default:
			t.Fatalf("unexpected claim result: fresh=%v share=%#v err=%v", got.fresh, got.share, got.err)
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("claim outcomes: winners=%d losers=%d, want 1/1", winners, losers)
	}
}

func TestVideoShareClaimCannotBeRepeatedAndPlaybackSessionExpires(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	seedShareTestVideo(t, cat, "video-2")

	now := time.Now().Truncate(time.Millisecond)
	expiresAt := now.Add(24 * time.Hour)
	if err := cat.CreateVideoShare(ctx, "share-2", "token-hash-2", "video-2", now); err != nil {
		t.Fatalf("create share: %v", err)
	}
	first, fresh, err := cat.ClaimVideoShare(ctx, "token-hash-2", "session-a", now, expiresAt)
	if err != nil || !fresh {
		t.Fatalf("first claim fresh=%v err=%v", fresh, err)
	}
	if first.ID != "share-2" || first.VideoID != "video-2" {
		t.Fatalf("first claim = %#v", first)
	}

	if _, _, err := cat.ClaimVideoShare(ctx, "token-hash-2", "session-a", now.Add(time.Minute), now.Add(25*time.Hour)); !errors.Is(err, ErrVideoShareConsumed) {
		t.Fatalf("same-session retry error = %v, want consumed", err)
	}
	if _, _, err := cat.ClaimVideoShare(ctx, "token-hash-2", "session-b", now.Add(time.Minute), now.Add(25*time.Hour)); !errors.Is(err, ErrVideoShareConsumed) {
		t.Fatalf("other-session claim error = %v, want consumed", err)
	}

	videoID, err := cat.ActiveVideoShare(ctx, "share-2", "session-a", expiresAt.Add(-time.Millisecond))
	if err != nil || videoID != "video-2" {
		t.Fatalf("active share video=%q err=%v", videoID, err)
	}
	if _, err := cat.ActiveVideoShare(ctx, "share-2", "session-a", expiresAt); !errors.Is(err, ErrVideoShareUnavailable) {
		t.Fatalf("expired share error = %v, want unavailable", err)
	}
	if err := cat.CreateVideoShare(ctx, "share-2b", "token-hash-2b", "video-2", expiresAt); err != nil {
		t.Fatalf("create next share: %v", err)
	}
	if _, _, err := cat.videoShareByTokenHash(ctx, "token-hash-2"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired consumed share cleanup error = %v, want no rows", err)
	}
}

func TestDeletingVideoDeletesItsShareLinks(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	seedShareTestVideo(t, cat, "video-3")

	now := time.Now().Truncate(time.Millisecond)
	if err := cat.CreateVideoShare(ctx, "share-3", "token-hash-3", "video-3", now); err != nil {
		t.Fatalf("create share: %v", err)
	}
	if err := cat.DeleteVideo(ctx, "video-3"); err != nil {
		t.Fatalf("delete video: %v", err)
	}
	if _, _, err := cat.ClaimVideoShare(ctx, "token-hash-3", "session-a", now, now.Add(time.Hour)); !errors.Is(err, ErrVideoShareUnavailable) {
		t.Fatalf("claim deleted video's share error = %v, want unavailable", err)
	}
}

func seedShareTestVideo(t *testing.T, cat *Catalog, id string) {
	t.Helper()
	now := time.Now()
	if err := cat.UpsertVideo(context.Background(), &Video{
		ID:          id,
		DriveID:     "drive-1",
		FileID:      "file-1",
		Title:       "Share test video",
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed video: %v", err)
	}
}
