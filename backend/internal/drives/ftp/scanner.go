package ftp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

// DefaultAuthor is the author tag assigned to FTP-imported videos.
const DefaultAuthor = "FTP"

// DefaultTag is the tag assigned to FTP-imported videos.
const DefaultTag = "FTP"

var videoExts = map[string]struct{}{
	".mp4": {}, ".mkv": {}, ".mov": {}, ".m4v": {},
	".webm": {}, ".avi": {}, ".flv": {}, ".wmv": {},
}

// ScanConfig is the dependency injection for an FTP scanner.
type ScanConfig struct {
	Driver  *Driver
	Catalog *catalog.Catalog
}

// Scanner scans an FTP server for video files and adds them to the catalog.
type Scanner struct {
	cfg ScanConfig
	mu  sync.Mutex
}

// NewScanner constructs a Scanner.
func NewScanner(cfg ScanConfig) *Scanner {
	return &Scanner{cfg: cfg}
}

// ScanResult reports the outcome of an FTP scan.
type ScanResult struct {
	FilesFound int
	NewVideos  int
	Skipped    int
	Failed     int
}

// Run connects to the FTP server, recursively lists all video files under
// the configured remote root path, and upserts them into the catalog.
//
// The remote root is read from the catalog drive's root_id field.
// Videos are NOT downloaded — only cataloged. Actual download happens
// on-demand via StreamURL.
func (s *Scanner) Run(ctx context.Context, remoteRoot string) (*ScanResult, error) {
	if s.cfg.Driver == nil {
		return nil, errors.New("ftp scanner: driver not set")
	}
	if s.cfg.Catalog == nil {
		return nil, errors.New("ftp scanner: catalog not set")
	}

	result := &ScanResult{}

	conn, err := dialFTP(ctx, s.cfg.Driver.Addr(), s.cfg.Driver.username, s.cfg.Driver.password)
	if err != nil {
		return nil, fmt.Errorf("ftp scanner: connect: %w", err)
	}
	defer func() { _ = conn.Quit() }()

	// Collect all video files recursively
	var videoFiles []ftpEntry
	if err := s.walk(ctx, conn, remoteRoot, &videoFiles); err != nil {
		return nil, fmt.Errorf("ftp scanner: walk %s: %w", remoteRoot, err)
	}
	result.FilesFound = len(videoFiles)

	driveID := s.cfg.Driver.ID()

	for _, entry := range videoFiles {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		// Build remote relative path
		remotePath := filepath.ToSlash(filepath.Join(remoteRoot, entry.Name))
		fileID := EncodeFileID(remotePath)

		// Check if already in catalog
		videoID := buildFTPVideoID(driveID, fileID)
		if existing, _ := s.cfg.Catalog.GetVideo(ctx, videoID); existing != nil {
			result.Skipped++
			continue
		}
		// Check by file_id (another FTP drive might have same path)
		if existing, _ := s.cfg.Catalog.FindVideoByFileSignature(ctx, fileID, entry.Size); existing != nil {
			result.Skipped++
			continue
		}

		// Insert into catalog
		title := videoTitleFromPath(entry.Name)
		ext := strings.ToLower(filepath.Ext(entry.Name))
		tags := []string{DefaultTag}
		if matched, err := s.cfg.Catalog.MatchTags(ctx, title+" "+DefaultAuthor); err == nil {
			tags = mergeTags(tags, matched)
		}

		now := time.Now()
		v := &catalog.Video{
			ID:            videoID,
			DriveID:       driveID,
			FileID:        fileID,
			FileName:      entry.Name,
			Title:         title,
			Author:        DefaultAuthor,
			Tags:          tags,
			Size:          entry.Size,
			Ext:           strings.TrimPrefix(ext, "."),
			Quality:       "HD",
			PreviewStatus: "pending",
			PublishedAt:   now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if err := s.cfg.Catalog.UpsertVideo(ctx, v); err != nil {
			log.Printf("[ftp] drive=%s upsert %s: %v", driveID, entry.Name, err)
			result.Failed++
			continue
		}
		result.NewVideos++
		log.Printf("[ftp] drive=%s new video: %s (%d bytes)", driveID, entry.Name, entry.Size)
	}

	return result, nil
}

// walk recursively lists the FTP server starting at dir and appends video files.
func (s *Scanner) walk(ctx context.Context, conn *ftpConn, dir string, files *[]ftpEntry) error {
	dir = filepath.ToSlash(strings.TrimSpace(dir))
	if dir == "" {
		dir = "/"
	}

	entries, err := conn.List(dir)
	if err != nil {
		return fmt.Errorf("list %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}
		fullPath := filepath.ToSlash(filepath.Join(dir, entry.Name))

		if entry.IsDir {
			if err := s.walk(ctx, conn, fullPath, files); err != nil {
				// Log but don't fail — a single unreadable subdirectory shouldn't stop the entire scan
				log.Printf("[ftp] drive=%s skip dir %s: %v", s.cfg.Driver.ID(), fullPath, err)
			}
		} else {
			ext := strings.ToLower(filepath.Ext(entry.Name))
			if _, ok := videoExts[ext]; ok {
				*files = append(*files, entry)
			}
		}
	}
	return nil
}

// BuildFTPVideoID constructs a catalog video ID from drive ID and file ID.
func BuildFTPVideoID(driveID, fileID string) string {
	return buildFTPVideoID(driveID, fileID)
}

func buildFTPVideoID(driveID, fileID string) string {
	return Kind + "-" + driveID + "-" + fileID
}

func videoTitleFromPath(path string) string {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	title := strings.TrimSuffix(name, ext)
	// Replace common separators with spaces
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return name
	}
	return title
}

func mergeTags(lists ...[]string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, list := range lists {
		for _, tag := range list {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			key := strings.ToLower(tag)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, tag)
		}
	}
	return out
}
