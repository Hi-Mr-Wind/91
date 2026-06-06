package spider91

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/mediaasset"
)

// ImporterConfig is the dependency injection for Importer.
type ImporterConfig struct {
	// Driver is the spider91 driver for file path resolution.
	Driver *Driver
	// Catalog for dedup checking and upsert.
	Catalog *catalog.Catalog
	// CommonThumbDir is the shared thumbs directory for /p/thumb/ routes.
	CommonThumbDir string
	// HTTPClient for downloading videos and thumbnails; nil uses default.
	HTTPClient *http.Client
	// DownloadTimeout limits single video/thumb download duration.
	DownloadTimeout time.Duration
	// OnNewVideo is called after a video is successfully imported.
	OnNewVideo func(v *catalog.Video)
}

// Importer imports videos from pre-existing JSON entries (e.g., from a spider
// crawl output file or manually constructed JSON).
type Importer struct {
	cfg ImporterConfig
}

// NewImporter constructs an Importer.
func NewImporter(cfg ImporterConfig) *Importer {
	if cfg.DownloadTimeout <= 0 {
		cfg.DownloadTimeout = 30 * time.Minute
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			// No global timeout — each download is controlled via ctx.
			Transport: &http.Transport{
				ResponseHeaderTimeout: 60 * time.Second,
				MaxIdleConns:          10,
				IdleConnTimeout:       90 * time.Second,
			},
		}
	}
	return &Importer{cfg: cfg}
}

// ImportResult aggregates the outcome of ImportEntries.
type ImportResult struct {
	Total   int
	New     int
	Skipped int
	Failed  int
}

// ImportEntries processes a batch of SpiderVideoEntry items:
//  1. Skips entries that already exist in the catalog (by videoID).
//  2. Downloads video + thumbnail to the spider91 driver directories.
//  3. Upserts into the catalog.
//
// It does NOT run the Python spider — the caller is responsible for providing
// the entries (parsed from JSON).
func (imp *Importer) ImportEntries(ctx context.Context, entries []SpiderVideoEntry) (*ImportResult, error) {
	if imp.cfg.Driver == nil {
		return nil, errors.New("spider91 importer: driver not set")
	}
	if imp.cfg.Catalog == nil {
		return nil, errors.New("spider91 importer: catalog not set")
	}
	if err := imp.cfg.Driver.Init(ctx); err != nil {
		return nil, fmt.Errorf("spider91 importer: driver init: %w", err)
	}

	result := &ImportResult{Total: len(entries)}

	for i, item := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		sourceID := sourceIDForItem(item)
		if sourceID == "" || strings.TrimSpace(item.VideoURL) == "" {
			result.Failed++
			log.Printf("[spider91:import] entry %d: empty source_id or video_url, skipping", i)
			continue
		}

		videoID := buildVideoID(imp.cfg.Driver.ID(), sourceID)
		if existing, _ := imp.cfg.Catalog.GetVideo(ctx, videoID); existing != nil {
			result.Skipped++
			log.Printf("[spider91:import] video %s already exists, skipping", videoID)
			continue
		}

		if err := imp.processOne(ctx, videoID, item); err != nil {
			log.Printf("[spider91:import] video %s failed: %v", videoID, err)
			result.Failed++
			continue
		}
		result.New++
		_ = i // suppress unused warning
	}
	return result, nil
}

// processOne handles a single entry: download + upsert.
// It is deliberately similar to Crawler.processOne to keep behavior consistent.
func (imp *Importer) processOne(ctx context.Context, videoID string, item SpiderVideoEntry) error {
	sourceID := sourceIDForItem(item)
	if sourceID == "" {
		return errors.New("empty numeric source id")
	}

	videoURL := strings.TrimSpace(item.VideoURL)
	videoSourceID := sourceIDFromVideoURL(videoURL)
	if videoSourceID == "" {
		return fmt.Errorf("video url has no numeric source id: %s", videoURL)
	}
	if videoSourceID != sourceID {
		return fmt.Errorf("video source id mismatch: got %s want %s", videoSourceID, sourceID)
	}
	thumbURL := normalizeThumbURLForSource(item.ThumbURL, sourceID)

	videoExt := detectVideoExt(videoURL)
	videoFile := sourceID + videoExt
	thumbFile := sourceID + detectThumbExt(thumbURL)

	videoPath, err := imp.cfg.Driver.VideoPath(videoFile)
	if err != nil {
		return err
	}
	thumbPath, err := imp.cfg.Driver.ThumbPath(thumbFile)
	if err != nil {
		return err
	}

	// Download video (required)
	dlCtx, cancel := imp.downloadContext(ctx)
	videoSize, err := downloadAtomic(dlCtx, imp.cfg.HTTPClient, videoURL, videoPath, item.DetailURL)
	cancel()
	if err != nil {
		return fmt.Errorf("download video: %w", err)
	}

	// Download thumbnail (best-effort)
	thumbReady := false
	if strings.TrimSpace(thumbURL) != "" {
		thumbCtx, thumbCancel := imp.downloadContext(ctx)
		_, err := downloadAtomic(thumbCtx, imp.cfg.HTTPClient, thumbURL, thumbPath, item.DetailURL)
		thumbCancel()
		if err != nil {
			log.Printf("[spider91:import] viewkey=%s source_id=%s thumb download failed: %v", item.Viewkey, sourceID, err)
		} else {
			thumbReady = true
		}
	}

	// Copy thumb to common dir
	if thumbReady && imp.cfg.CommonThumbDir != "" {
		if err := os.MkdirAll(imp.cfg.CommonThumbDir, 0o755); err != nil {
			log.Printf("[spider91:import] mkdir common thumbs: %v", err)
			thumbReady = false
		} else {
			dst := mediaasset.ThumbnailPathInDir(imp.cfg.CommonThumbDir, videoID)
			if err := copyFileAtomic(thumbPath, dst); err != nil {
				log.Printf("[spider91:import] copy thumb to common dir: %v", err)
				thumbReady = false
			}
		}
	}

	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = sourceID
	}
	tags := []string{DefaultTag}
	if matched, err := imp.cfg.Catalog.MatchTags(ctx, title+" "+DefaultAuthor); err == nil {
		tags = mergeCatalogTags(tags, matched)
	}

	now := time.Now()
	v := &catalog.Video{
		ID:            videoID,
		DriveID:       imp.cfg.Driver.ID(),
		FileID:        videoFile,
		FileName:      videoFile,
		Title:         title,
		Author:        DefaultAuthor,
		Tags:          tags,
		Ext:           strings.TrimPrefix(videoExt, "."),
		Quality:       "HD",
		Size:          videoSize,
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if thumbReady {
		v.ThumbnailURL = "/p/thumb/" + v.ID
	}
	if err := imp.cfg.Catalog.UpsertVideo(ctx, v); err != nil {
		_ = os.Remove(videoPath)
		_ = os.Remove(thumbPath)
		return fmt.Errorf("upsert video: %w", err)
	}
	if !thumbReady {
		_ = imp.cfg.Catalog.UpdateVideoMeta(ctx, v.ID, catalog.VideoMetaPatch{
			ThumbnailStatus: "failed",
		})
	}
	if imp.cfg.OnNewVideo != nil {
		imp.cfg.OnNewVideo(v)
	}
	log.Printf("[spider91:import] drive=%s viewkey=%s source_id=%s ok title=%q size=%d",
		imp.cfg.Driver.ID(), item.Viewkey, sourceID, v.Title, v.Size)
	return nil
}

func (imp *Importer) downloadContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if imp.cfg.DownloadTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, imp.cfg.DownloadTimeout)
}

// downloadAtomic is a standalone download helper shared by import and crawl paths.
// It downloads src URL to dst (via .part + rename) and returns the written size.
func downloadAtomic(ctx context.Context, client *http.Client, src, dst, referer string) (int64, error) {
	if strings.TrimSpace(src) == "" {
		return 0, errors.New("empty url")
	}
	if _, err := os.Stat(src); err == nil {
		// src looks like a local path — not a URL. This shouldn't happen in normal
		// import flow, but guard against it so we don't try to HTTP-GET a file path.
		return 0, fmt.Errorf("url appears to be a local path: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", downloadUA)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, &downloadHTTPError{StatusCode: resp.StatusCode}
	}

	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return 0, closeErr
	}
	if written <= 0 {
		_ = os.Remove(tmp)
		return 0, errors.New("empty body")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return written, nil
}
