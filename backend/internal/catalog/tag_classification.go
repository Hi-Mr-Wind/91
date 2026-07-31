package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/video-site/backend/internal/tagging"
)

// removeAutomaticTaggingArtifacts removes the retired "create new labels from
// content" model. It preserves builtin/user tag definitions plus crawler-owned
// tags, and leaves engine assignments that point at preserved tags for the
// subsequent existing-tag retag pass to refresh.
func (c *Catalog) removeAutomaticTaggingArtifacts(ctx context.Context) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	generatedTagFilter := `
SELECT t.id
  FROM tags t
 WHERE lower(trim(COALESCE(t.source, ''))) = 'generated'
   AND lower(trim(COALESCE(t.origin, ''))) != 'crawler'
   AND lower(trim(COALESCE(t.origin, ''))) != '` + avSeriesOrigin + `'
   AND NOT EXISTS (
     SELECT 1
       FROM video_tags vt_crawler
      WHERE vt_crawler.tag_id = t.id
        AND lower(trim(COALESCE(vt_crawler.source, ''))) = 'crawler'
   )`

	affectedRows, err := tx.QueryContext(ctx, `
SELECT DISTINCT vt.video_id
  FROM video_tags vt
  LEFT JOIN tags t ON t.id = vt.tag_id
 WHERE lower(trim(COALESCE(vt.source, ''))) IN ('series', 'propagated')
    OR vt.tag_id IN (`+generatedTagFilter+`)`)
	if err != nil {
		return err
	}
	var videoIDs []string
	for affectedRows.Next() {
		var videoID string
		if err := affectedRows.Scan(&videoID); err != nil {
			affectedRows.Close()
			return err
		}
		videoIDs = append(videoIDs, videoID)
	}
	if err := affectedRows.Err(); err != nil {
		affectedRows.Close()
		return err
	}
	if err := affectedRows.Close(); err != nil {
		return err
	}

	removedAssignments := int64(0)
	res, err := tx.ExecContext(ctx, `
DELETE FROM video_tags
 WHERE lower(trim(COALESCE(source, ''))) IN ('series', 'propagated')`)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil {
		removedAssignments += n
	}
	res, err = tx.ExecContext(ctx, `DELETE FROM video_tags WHERE tag_id IN (`+generatedTagFilter+`)`)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil {
		removedAssignments += n
	}

	res, err = tx.ExecContext(ctx, `DELETE FROM tags WHERE id IN (`+generatedTagFilter+`)`)
	if err != nil {
		return err
	}
	removedTags, _ := res.RowsAffected()

	staleRows, err := tx.QueryContext(ctx, `
SELECT id
  FROM videos
 WHERE COALESCE(tags_manual, 0) = 0
   AND COALESCE(tags, '') NOT IN ('', '[]', 'null')
   AND NOT EXISTS (
     SELECT 1
       FROM video_tags vt
      WHERE vt.video_id = videos.id
   )`)
	if err != nil {
		return err
	}
	for staleRows.Next() {
		var videoID string
		if err := staleRows.Scan(&videoID); err != nil {
			staleRows.Close()
			return err
		}
		videoIDs = append(videoIDs, videoID)
	}
	if err := staleRows.Err(); err != nil {
		staleRows.Close()
		return err
	}
	if err := staleRows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE videos
   SET tags = '[]'
 WHERE COALESCE(tags_manual, 0) = 0
   AND COALESCE(tags, '') NOT IN ('', '[]', 'null')
   AND NOT EXISTS (
     SELECT 1
       FROM video_tags vt
      WHERE vt.video_id = videos.id
   )`); err != nil {
		return err
	}

	for _, videoID := range uniqueStrings(videoIDs) {
		manual := hasManualTagsTx(ctx, tx, videoID)
		if err := syncVideoTagsJSONTx(ctx, tx, videoID, manual); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value, updated_at) VALUES (?, 'false', ?)
ON CONFLICT(key) DO UPDATE SET
  value = 'false',
  updated_at = excluded.updated_at`, settingAutoGenerateTagsEnabled, time.Now().UnixMilli()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	if removedAssignments > 0 || removedTags > 0 {
		log.Printf("[catalog] removed retired automatic tagging artifacts: assignments=%d tags=%d", removedAssignments, removedTags)
		if removedTags > 0 {
			if err := c.bumpTagRulesVersion(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// classifyAllTagsAddOnly 用当前标签池对全库做一次"只增不减"的分类：
// 给每个非人工锁定视频补上缺失的命中标签，不移除任何已有标签。
// HTTP 监听完成后在后台执行，保证新加的内置标签对存量视频生效且不阻塞启动；
// 完整的重算走 retag job。
func (c *Catalog) classifyAllTagsAddOnly(ctx context.Context) error {
	matcher, err := c.Matcher(ctx)
	if err != nil {
		return err
	}
	if len(matcher.Labels()) == 0 {
		return nil
	}

	existing, err := c.loadVideoTagLabelSets(ctx)
	if err != nil {
		return err
	}

	rows, err := c.db.QueryContext(ctx, `
SELECT id, title, COALESCE(author, ''), COALESCE(file_name, ''), COALESCE(dir_name, ''), COALESCE(tags_manual, 0)
FROM videos`)
	if err != nil {
		return err
	}
	type pendingVideo struct {
		id      string
		matches []tagging.Match
	}
	var pending []pendingVideo
	for rows.Next() {
		var videoID, title, author, fileName, dirName string
		var manual int
		if err := rows.Scan(&videoID, &title, &author, &fileName, &dirName, &manual); err != nil {
			rows.Close()
			return err
		}
		if manual == 1 {
			continue
		}
		matches := matcher.Match(matchFields(title, fileName, author, dirName)...)
		if len(matches) == 0 {
			continue
		}
		have := existing[videoID]
		var missing []tagging.Match
		for _, m := range matches {
			if _, ok := have[strings.ToLower(m.Label)]; !ok {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			pending = append(pending, pendingVideo{id: videoID, matches: missing})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	total := 0
	for _, p := range pending {
		changed := false
		for _, m := range p.matches {
			tag, err := c.getTagByLabel(ctx, m.Label)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			if err := c.insertVideoTag(ctx, p.id, tag.ID, "auto", m.Evidence()); err != nil {
				return err
			}
			changed = true
			total++
		}
		if changed {
			if err := c.syncVideoTagsJSON(ctx, p.id, false); err != nil {
				return err
			}
		}
	}
	if total > 0 {
		log.Printf("[catalog] classified %d missing video tag(s) in post-startup background job", total)
	}
	return nil
}

// matchFields 组装匹配材料，顺序即证据优先级。
func matchFields(title, fileName, author, dirName string) []tagging.Field {
	return []tagging.Field{
		{Name: "标题", Text: title},
		{Name: "文件名", Text: fileName},
		{Name: "作者", Text: author},
		{Name: "目录", Text: dirName},
	}
}

// loadVideoTagLabelSets 一次性载入全部视频当前已挂标签（video_id → 小写 label 集合）。
func (c *Catalog) loadVideoTagLabelSets(ctx context.Context) (map[string]map[string]struct{}, error) {
	rows, err := c.db.QueryContext(ctx, `
SELECT vt.video_id, t.label
FROM video_tags vt
JOIN tags t ON t.id = vt.tag_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]struct{}{}
	for rows.Next() {
		var videoID, label string
		if err := rows.Scan(&videoID, &label); err != nil {
			return nil, err
		}
		set := out[videoID]
		if set == nil {
			set = map[string]struct{}{}
			out[videoID] = set
		}
		set[strings.ToLower(label)] = struct{}{}
	}
	return out, rows.Err()
}

func (c *Catalog) backfillVideoTags(ctx context.Context) error {
	rows, err := c.db.QueryContext(ctx, `
SELECT id, COALESCE(tags, '[]')
FROM videos
WHERE COALESCE(tags, '') NOT IN ('', '[]', 'null')
  AND NOT EXISTS (
	SELECT 1
	  FROM video_tags vt
	 WHERE vt.video_id = videos.id
  )`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var videoID, tagsJSON string
		if err := rows.Scan(&videoID, &tagsJSON); err != nil {
			return err
		}
		var labels []string
		if err := json.Unmarshal([]byte(tagsJSON), &labels); err != nil {
			continue
		}
		if len(labels) == 0 {
			continue
		}
		added, err := c.addVideoTags(ctx, videoID, labels, "legacy", true)
		if err != nil {
			return err
		}
		if added {
			if err := c.syncVideoTagsJSON(ctx, videoID, false); err != nil {
				return err
			}
		}
	}
	return nil
}
