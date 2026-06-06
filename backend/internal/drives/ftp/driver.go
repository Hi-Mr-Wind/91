// Package ftp exposes a remote FTP server directory as a Drive.
// Videos are downloaded from the FTP server to a local cache on first access.
package ftp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/video-site/backend/internal/drives"
)

const Kind = "ftp"

// Config is the dependency injection for creating an FTP Driver.
type Config struct {
	ID       string // catalog drive id
	RootDir  string // local cache root: <data>/ftp/<driveID>/
	Host     string // FTP server host
	Port     int    // FTP server port (default 21)
	Username string
	Password string
}

// Driver implements drives.Drive for FTP sources.
type Driver struct {
	id       string
	rootDir  string
	host     string
	port     int
	username string
	password string
}

// New constructs a Driver.
func New(c Config) *Driver {
	if c.Port <= 0 {
		c.Port = 21
	}
	return &Driver{
		id:       c.ID,
		rootDir:  c.RootDir,
		host:     c.Host,
		port:     c.Port,
		username: c.Username,
		password: c.Password,
	}
}

// Kind returns "ftp".
func (d *Driver) Kind() string { return Kind }

// ID returns the catalog drive id.
func (d *Driver) ID() string { return d.id }

// RootID returns "/" since FTP doesn't have a native root directory ID concept.
func (d *Driver) RootID() string { return "/" }

// Host returns the configured FTP host.
func (d *Driver) Host() string { return d.host }

// Port returns the configured FTP port.
func (d *Driver) Port() int { return d.port }

// Addr returns "host:port".
func (d *Driver) Addr() string { return fmt.Sprintf("%s:%d", d.host, d.port) }

// Credentials returns the FTP login credentials.
func (d *Driver) Credentials() (user, pass string) { return d.username, d.password }

// VideosDir returns the local cache directory for video files.
func (d *Driver) VideosDir() string { return filepath.Join(d.rootDir, "videos") }

// RootDir returns the driver's local storage root.
func (d *Driver) RootDir() string { return d.rootDir }

// Init creates the local cache directory and verifies FTP connectivity.
func (d *Driver) Init(ctx context.Context) error {
	if strings.TrimSpace(d.host) == "" {
		return errors.New("ftp: host is required")
	}
	if strings.TrimSpace(d.rootDir) == "" {
		return errors.New("ftp: empty root dir")
	}
	if err := os.MkdirAll(d.VideosDir(), 0o755); err != nil {
		return fmt.Errorf("ftp: init videos dir: %w", err)
	}
	// Verify connectivity
	conn, err := dialFTP(ctx, d.Addr(), d.username, d.password)
	if err != nil {
		return fmt.Errorf("ftp: connect %s: %w", d.Addr(), err)
	}
	_ = conn.Quit()
	return nil
}

// List lists files in the local cache directory (used for GC/audit, not for scanning).
func (d *Driver) List(ctx context.Context, dirID string) ([]drives.Entry, error) {
	entries, err := os.ReadDir(d.VideosDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]drives.Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, drives.Entry{
			ID:      e.Name(),
			Name:    e.Name(),
			Size:    info.Size(),
			IsDir:   false,
			ModTime: info.ModTime(),
		})
	}
	return out, nil
}

// Stat returns metadata for a single locally-cached video file.
func (d *Driver) Stat(ctx context.Context, fileID string) (*drives.Entry, error) {
	path, err := d.videoPath(fileID)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &drives.Entry{
		ID:      fileID,
		Name:    fileID,
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

// StreamURL returns the local file path for a video. If the file is not yet
// cached locally, it downloads it from the FTP server first.
func (d *Driver) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	path, err := d.videoPath(fileID)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// File not cached yet — download from FTP server.
		// The fileID encodes the remote relative path.
		remotePath, decErr := decodeFileID(fileID)
		if decErr != nil {
			return nil, fmt.Errorf("ftp: decode file id %q: %w", fileID, decErr)
		}
		if dlErr := d.downloadFile(ctx, remotePath, path); dlErr != nil {
			return nil, fmt.Errorf("ftp: download %s: %w", remotePath, dlErr)
		}
		info, err = os.Stat(path)
		if err != nil {
			return nil, err
		}
	}
	if info.IsDir() || info.Size() == 0 {
		return nil, os.ErrNotExist
	}
	return &drives.StreamLink{
		URL:     path,
		Expires: time.Now().Add(24 * time.Hour),
	}, nil
}

// Upload is not supported for FTP drives (one-way: FTP→local).
func (d *Driver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	return "", drives.ErrNotSupported
}

// EnsureDir is not supported.
func (d *Driver) EnsureDir(ctx context.Context, pathFromRoot string) (string, error) {
	return "", drives.ErrNotSupported
}

// videoPath returns the safe local path for a cached video file.
func (d *Driver) videoPath(fileID string) (string, error) {
	id := strings.TrimSpace(fileID)
	if id == "" || filepath.Base(id) != id {
		return "", errors.New("ftp: invalid file id")
	}
	absDir, err := filepath.Abs(d.VideosDir())
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(filepath.Join(absDir, id))
	if err != nil {
		return "", err
	}
	if absPath != absDir && !strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) {
		return "", errors.New("ftp: file id escapes root")
	}
	return absPath, nil
}

// downloadFile fetches a single file from the FTP server to the local cache.
func (d *Driver) downloadFile(ctx context.Context, remotePath, localPath string) error {
	conn, err := dialFTP(ctx, d.Addr(), d.username, d.password)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Quit() }()

	r, err := conn.Retr(remotePath)
	if err != nil {
		return fmt.Errorf("ftp: retr %s: %w", remotePath, err)
	}
	defer r.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	tmp := localPath + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, r)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if written <= 0 {
		_ = os.Remove(tmp)
		return errors.New("ftp: downloaded empty file")
	}
	return os.Rename(tmp, localPath)
}

// EncodeFileID encodes a remote relative path into a safe file ID.
func EncodeFileID(remoteRelPath string) string {
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(remoteRelPath)))
	return base64.RawURLEncoding.EncodeToString([]byte(cleaned))
}

// DecodeFileID decodes a file ID back to the remote relative path.
func DecodeFileID(id string) (string, error) {
	return decodeFileID(id)
}

func decodeFileID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("ftp: empty file id")
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", fmt.Errorf("ftp: decode file id: %w", err)
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(string(raw))))
	if rel == "." || rel == "" {
		return "", errors.New("ftp: invalid remote path")
	}
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return "", errors.New("ftp: path escapes root")
	}
	return rel, nil
}

var _ drives.Drive = (*Driver)(nil)
