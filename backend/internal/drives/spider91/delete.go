package spider91

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeleteLocalAssets removes the local video file and thumbnail file
// for a spider91 video identified by its fileID (e.g., "1210742.mp4").
// It does NOT touch the catalog database — that's the caller's responsibility.
//
// On success it returns the list of paths that were removed (may be empty
// if files were already gone). Errors from os.Remove are only returned
// for unexpected failures; os.ErrNotExist is silently ignored.
func (d *Driver) DeleteLocalAssets(fileID string) ([]string, error) {
	if strings.TrimSpace(fileID) == "" {
		return nil, errors.New("spider91: delete local assets: empty file id")
	}
	var removed []string

	videoPath, err := d.VideoPath(fileID)
	if err != nil {
		return nil, fmt.Errorf("spider91: delete local assets: video path: %w", err)
	}
	if err := os.Remove(videoPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("spider91: remove video file %s: %w", videoPath, err)
	} else if err == nil {
		removed = append(removed, videoPath)
	}

	thumbPath, err := d.ThumbPath(fileID)
	if err != nil {
		return removed, fmt.Errorf("spider91: delete local assets: thumb path: %w", err)
	}
	if err := os.Remove(thumbPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("spider91: remove thumb file %s: %w", thumbPath, err)
	} else if err == nil {
		removed = append(removed, thumbPath)
	}

	return removed, nil
}

// RemoveDriveDir deletes the entire drive root directory (videos/ and thumbs/).
// Only call this when deleting the drive itself.
func (d *Driver) RemoveDriveDir() error {
	if d.rootDir == "" {
		return errors.New("spider91: remove drive dir: empty root dir")
	}
	abs, err := filepath.Abs(d.rootDir)
	if err != nil {
		return fmt.Errorf("spider91: remove drive dir: abs: %w", err)
	}
	// Safety: rootDir must contain "spider91" in its path
	if !strings.Contains(filepath.ToSlash(abs), "/spider91/") && !strings.HasSuffix(filepath.ToSlash(abs), "/spider91") {
		return fmt.Errorf("spider91: remove drive dir: path does not look like a spider91 directory: %s", abs)
	}
	if err := os.RemoveAll(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("spider91: remove drive dir: %w", err)
	}
	return nil
}
