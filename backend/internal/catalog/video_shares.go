package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

var (
	ErrVideoShareUnavailable = errors.New("video share unavailable")
	ErrVideoShareConsumed    = errors.New("video share already consumed")
)

// VideoShare contains only server-side share state. TokenHash and SessionHash
// are deliberately not exposed so callers cannot accidentally serialize them.
type VideoShare struct {
	ID               string
	VideoID          string
	CreatedAt        time.Time
	ConsumedAt       time.Time
	SessionExpiresAt time.Time
}

func (c *Catalog) CreateVideoShare(
	ctx context.Context,
	id string,
	tokenHash string,
	videoID string,
	createdAt time.Time,
) error {
	id = strings.TrimSpace(id)
	tokenHash = strings.TrimSpace(tokenHash)
	videoID = strings.TrimSpace(videoID)
	if id == "" || tokenHash == "" || videoID == "" {
		return ErrVideoShareUnavailable
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	// Used links no longer serve any purpose after their playback session has
	// expired. Opportunistic cleanup keeps the table bounded in normal use while
	// leaving never-opened links valid until somebody claims them.
	if _, err := c.db.ExecContext(ctx, `
DELETE FROM video_shares
 WHERE consumed_at > 0
   AND session_expires_at <= ?`, createdAt.UnixMilli()); err != nil {
		return err
	}
	res, err := c.db.ExecContext(ctx, `
INSERT INTO video_shares (id, token_hash, video_id, created_at)
SELECT ?, ?, id, ?
  FROM videos
 WHERE id = ?
   AND COALESCE(hidden, 0) = 0`, id, tokenHash, createdAt.UnixMilli(), videoID)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return ErrVideoShareUnavailable
	}
	return nil
}

// ClaimVideoShare atomically binds an unused link to one playback session.
// Every later claim, including one carrying the session that already won,
// receives ErrVideoShareConsumed. This conditional UPDATE is the concurrency
// boundary that prevents refreshes or simultaneous opens from succeeding twice.
func (c *Catalog) ClaimVideoShare(
	ctx context.Context,
	tokenHash string,
	sessionHash string,
	now time.Time,
	sessionExpiresAt time.Time,
) (*VideoShare, bool, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	sessionHash = strings.TrimSpace(sessionHash)
	if tokenHash == "" || sessionHash == "" || !sessionExpiresAt.After(now) {
		return nil, false, ErrVideoShareUnavailable
	}

	res, err := c.db.ExecContext(ctx, `
UPDATE video_shares
   SET consumed_at = ?, session_hash = ?, session_expires_at = ?
 WHERE token_hash = ?
   AND consumed_at = 0
   AND EXISTS (
       SELECT 1
         FROM videos
        WHERE videos.id = video_shares.video_id
          AND COALESCE(videos.hidden, 0) = 0
   )`, now.UnixMilli(), sessionHash, sessionExpiresAt.UnixMilli(), tokenHash)
	if err != nil {
		return nil, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}

	share, _, err := c.videoShareByTokenHash(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrVideoShareUnavailable
	}
	if err != nil {
		return nil, false, err
	}
	if affected == 1 {
		return share, true, nil
	}
	if !share.ConsumedAt.IsZero() {
		return nil, false, ErrVideoShareConsumed
	}
	return nil, false, ErrVideoShareUnavailable
}

// ActiveVideoShare returns the sole video authorized by a claimed share
// session. The query also rejects hidden or deleted videos.
func (c *Catalog) ActiveVideoShare(
	ctx context.Context,
	shareID string,
	sessionHash string,
	now time.Time,
) (string, error) {
	var videoID string
	err := c.db.QueryRowContext(ctx, `
SELECT s.video_id
  FROM video_shares AS s
  JOIN videos AS v ON v.id = s.video_id
 WHERE s.id = ?
   AND s.consumed_at > 0
   AND s.session_hash = ?
   AND s.session_expires_at > ?
   AND COALESCE(v.hidden, 0) = 0`,
		strings.TrimSpace(shareID), strings.TrimSpace(sessionHash), now.UnixMilli()).Scan(&videoID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrVideoShareUnavailable
	}
	return videoID, err
}

func (c *Catalog) videoShareByTokenHash(ctx context.Context, tokenHash string) (*VideoShare, string, error) {
	var (
		share                                   VideoShare
		createdAt, consumedAt, sessionExpiresAt int64
		sessionHash                             string
	)
	err := c.db.QueryRowContext(ctx, `
SELECT id, video_id, created_at, consumed_at, session_hash, session_expires_at
  FROM video_shares
 WHERE token_hash = ?`, tokenHash).Scan(
		&share.ID,
		&share.VideoID,
		&createdAt,
		&consumedAt,
		&sessionHash,
		&sessionExpiresAt,
	)
	if err != nil {
		return nil, "", err
	}
	share.CreatedAt = time.UnixMilli(createdAt)
	if consumedAt > 0 {
		share.ConsumedAt = time.UnixMilli(consumedAt)
	}
	if sessionExpiresAt > 0 {
		share.SessionExpiresAt = time.UnixMilli(sessionExpiresAt)
	}
	return &share, sessionHash, nil
}
