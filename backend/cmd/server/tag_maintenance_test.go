package main

import (
	"context"
	"testing"
	"time"

	"github.com/video-site/backend/internal/catalog"
	"github.com/video-site/backend/internal/config"
)

func openServerTagMaintenanceCatalog(t *testing.T) (*catalog.Catalog, context.Context) {
	t.Helper()
	cat, err := catalog.Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() {
		if err := cat.Close(); err != nil {
			t.Fatalf("close catalog: %v", err)
		}
	})
	return cat, context.Background()
}

func seedServerTagVideo(t *testing.T, cat *catalog.Catalog, id, title string) {
	t.Helper()
	now := time.Now()
	if err := cat.UpsertVideo(context.Background(), &catalog.Video{
		ID:          id,
		DriveID:     "drive",
		FileID:      "file-" + id,
		FileName:    id + ".mp4",
		Title:       title,
		PublishedAt: now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func mustListTagsForServer(t *testing.T, ctx context.Context, cat *catalog.Catalog) []catalog.Tag {
	t.Helper()
	tags, err := cat.ListTags(ctx)
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	return tags
}

func serverHasTag(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func TestCollectTagTitleClustersUsesRelaxedTitleSimilarity(t *testing.T) {
	videos := []*catalog.Video{
		{ID: "a", Title: "abcdefghijklmnopqrst"},
		{ID: "b", Title: "abcdefghijklmnopqXYZ"},
		{ID: "c", Title: "完全不同的内容"},
	}
	groups := collectTagTitleClusters(videos)
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Fatalf("groups = %#v, want one two-video cluster", groups)
	}
}

func TestTitleClusterPropagationNoLongerWritesTags(t *testing.T) {
	cat, ctx := openServerTagMaintenanceCatalog(t)
	for _, id := range []string{"cluster-a", "cluster-b", "cluster-c", "cluster-manual"} {
		seedServerTagVideo(t, cat, id, "完全相同的系列标题")
	}
	for _, label := range []string{"cluster-tag", "locked-tag"} {
		if _, err := cat.EnsureTag(ctx, label, "user"); err != nil {
			t.Fatalf("ensure %s: %v", label, err)
		}
	}
	for _, id := range []string{"cluster-a", "cluster-b"} {
		if _, err := cat.AddVideoTagAssignments(ctx, id, []catalog.TagAssignment{{
			Label: "cluster-tag", Source: "auto", Evidence: "seed",
		}}); err != nil {
			t.Fatalf("seed tag %s: %v", id, err)
		}
	}
	if err := cat.SetManualVideoTags(ctx, "cluster-manual", []string{"locked-tag"}); err != nil {
		t.Fatalf("lock recipient: %v", err)
	}

	app := &App{cat: cat, cfg: &config.Config{}}
	added, err := app.propagateTagsAcrossTitleClusters(ctx)
	if err != nil {
		t.Fatalf("propagate title clusters: %v", err)
	}
	if added != 0 {
		t.Fatalf("added = %d, want none", added)
	}
	recipient, _ := cat.GetVideo(ctx, "cluster-c")
	if len(recipient.Tags) != 0 {
		t.Fatalf("recipient tags = %#v, want none", recipient.Tags)
	}
	locked, _ := cat.GetVideo(ctx, "cluster-manual")
	if len(locked.Tags) != 1 || locked.Tags[0] != "locked-tag" {
		t.Fatalf("manual recipient changed: %#v", locked.Tags)
	}
	metadata, err := cat.ListVideoTagMetadata(ctx, []string{"cluster-c"})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if len(metadata["cluster-c"]) != 0 {
		t.Fatalf("propagated metadata = %#v, want none", metadata["cluster-c"])
	}
}

func TestRunTagRetagWritesUpgradeMarkerOnlyAfterSuccess(t *testing.T) {
	cat, ctx := openServerTagMaintenanceCatalog(t)
	seedServerTagVideo(t, cat, "retag-video", "retag-keyword")
	if _, err := cat.EnsureTag(ctx, "retag-keyword", "user"); err != nil {
		t.Fatalf("ensure retag label: %v", err)
	}
	app := &App{cat: cat, cfg: &config.Config{}}
	if !app.beginTagJob("retag") {
		t.Fatal("begin retag job rejected")
	}
	app.runTagRetag(ctx, true)

	status := app.tagJobStatus()
	if status.State != "completed" || status.Running || status.Processed != 1 {
		t.Fatalf("status = %#v", status)
	}
	marker, err := cat.GetSetting(ctx, tagRetagUpgradeMarker, "")
	if err != nil || marker != "1" {
		t.Fatalf("marker = %q, %v", marker, err)
	}
	video, _ := cat.GetVideo(ctx, "retag-video")
	if len(video.Tags) != 1 || video.Tags[0] != "retag-keyword" {
		t.Fatalf("retagged labels = %#v, want retag-keyword", video.Tags)
	}
}

func TestRunTagRetagClearsRetiredAutoAssignmentsAndRefreshesExistingMatches(t *testing.T) {
	cat, ctx := openServerTagMaintenanceCatalog(t)
	seedServerTagVideo(t, cat, "disabled-retag-video", "disabled-keyword clip")
	if _, err := cat.EnsureTag(ctx, "disabled-keyword", "user"); err != nil {
		t.Fatalf("ensure user label: %v", err)
	}
	if _, err := cat.EnsureTag(ctx, "old-generated", "user"); err != nil {
		t.Fatalf("ensure old label: %v", err)
	}
	if _, err := cat.AddVideoTagAssignments(ctx, "disabled-retag-video", []catalog.TagAssignment{{
		Label: "old-generated", Source: "auto", Evidence: "old",
	}}); err != nil {
		t.Fatalf("seed old auto assignment: %v", err)
	}
	if err := cat.SetAutoGenerateTagsEnabled(ctx, false); err != nil {
		t.Fatalf("disable auto generate: %v", err)
	}

	app := &App{cat: cat, cfg: &config.Config{}}
	if !app.beginTagJob("retag") {
		t.Fatal("begin retag job rejected")
	}
	app.runTagRetag(ctx, false)

	status := app.tagJobStatus()
	if status.State != "completed" || status.Running || status.Processed != 1 {
		t.Fatalf("status = %#v", status)
	}
	video, err := cat.GetVideo(ctx, "disabled-retag-video")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if len(video.Tags) != 1 || video.Tags[0] != "disabled-keyword" {
		t.Fatalf("refreshed tags = %#v, want disabled-keyword", video.Tags)
	}
	foundOldLabel := false
	for _, tag := range mustListTagsForServer(t, ctx, cat) {
		if tag.Label == "old-generated" {
			foundOldLabel = true
		}
	}
	if !foundOldLabel {
		t.Fatal("user tag definition old-generated was removed")
	}
	if err := cat.SetAutoGenerateTagsEnabled(ctx, true); err != nil {
		t.Fatalf("re-enable auto generate: %v", err)
	}
	if _, err := cat.EnsureTag(ctx, "blocked-generated", "generated"); err != catalog.ErrAutoTagGenerationDisabled {
		t.Fatalf("generated tags should stay disabled, got %v", err)
	}
}

func TestPostStartupTagMaintenanceRunsBehindSharedJobState(t *testing.T) {
	cat, ctx := openServerTagMaintenanceCatalog(t)
	seedServerTagVideo(t, cat, "post-startup-video", "post-startup-keyword")
	for _, title := range []string{"FC2PPV-3259498", "FC2PPV-4162750", "FC2PPV-4768873"} {
		seedServerTagVideo(t, cat, "post-startup-"+title, title)
	}
	if _, err := cat.EnsureTag(ctx, "post-startup-keyword", "user"); err != nil {
		t.Fatalf("ensure label: %v", err)
	}
	if err := cat.SetSetting(ctx, tagRetagUpgradeMarker, "1"); err != nil {
		t.Fatalf("set completed upgrade marker: %v", err)
	}
	app := &App{cat: cat, cfg: &config.Config{}}
	if !app.beginTagJob("retag") {
		t.Fatal("begin post-startup job rejected")
	}
	app.runPostStartupTagMaintenance(ctx)

	status := app.tagJobStatus()
	if status.State != "completed" || status.Running || status.Processed != 4 || status.Total != 4 {
		t.Fatalf("status = %#v", status)
	}
	video, err := cat.GetVideo(ctx, "post-startup-video")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if len(video.Tags) != 1 {
		t.Fatalf("post-startup labels = %#v, want post-startup-keyword", video.Tags)
	}
	if video.Tags[0] != "post-startup-keyword" {
		t.Fatalf("post-startup labels = %#v, want post-startup-keyword", video.Tags)
	}
	seriesVideo, err := cat.GetVideo(ctx, "post-startup-FC2PPV-3259498")
	if err != nil {
		t.Fatalf("get series candidate: %v", err)
	}
	if !serverHasTag(seriesVideo.Tags, "AV") || !serverHasTag(seriesVideo.Tags, "FC2PPV") {
		t.Fatalf("series candidate tags = %#v, want AV + FC2PPV", seriesVideo.Tags)
	}
}

func TestStartPostStartupTagMaintenanceRunsDespiteCompletedUpgradeMarker(t *testing.T) {
	cat, ctx := openServerTagMaintenanceCatalog(t)
	seedServerTagVideo(t, cat, "post-startup-skip", "post-startup-skip-keyword")
	if _, err := cat.EnsureTag(ctx, "post-startup-skip-keyword", "user"); err != nil {
		t.Fatalf("ensure label: %v", err)
	}
	if err := cat.SetSetting(ctx, tagRetagUpgradeMarker, "1"); err != nil {
		t.Fatalf("set completed upgrade marker: %v", err)
	}
	app := &App{cat: cat, cfg: &config.Config{}}
	app.startPostStartupTagMaintenance(ctx)

	status := app.tagJobStatus()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status = app.tagJobStatus()
		if status.State == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.State != "completed" || status.Running || status.Total != 1 || status.Processed != 1 {
		t.Fatalf("post-startup tag job status = %#v, want completed", status)
	}
	video, err := cat.GetVideo(ctx, "post-startup-skip")
	if err != nil {
		t.Fatalf("get video: %v", err)
	}
	if len(video.Tags) != 1 || video.Tags[0] != "post-startup-skip-keyword" {
		t.Fatalf("post-startup restart labels = %#v, want post-startup-skip-keyword", video.Tags)
	}
}
