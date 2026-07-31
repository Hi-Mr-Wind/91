package catalog

import (
	"context"
	"strings"
	"testing"
)

func TestUpsertDriveUsesRootIDAsScanRootID(t *testing.T) {
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

	if err := cat.UpsertDrive(ctx, &Drive{
		ID:         "drive",
		Kind:       "p115",
		Name:       "115",
		RootID:     "root-folder",
		ScanRootID: "ignored-scan-root",
	}); err != nil {
		t.Fatalf("upsert drive: %v", err)
	}

	got, err := cat.GetDrive(ctx, "drive")
	if err != nil {
		t.Fatalf("get drive: %v", err)
	}
	if got.RootID != "root-folder" {
		t.Fatalf("rootId = %q, want root-folder", got.RootID)
	}
	if got.ScanRootID != "root-folder" {
		t.Fatalf("scanRootId = %q, want root-folder", got.ScanRootID)
	}
}

func TestUpsertDriveDefaultsRootIDByKind(t *testing.T) {
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

	cases := []struct {
		id   string
		kind string
		want string
	}{
		{id: "p115", kind: "p115", want: "0"},
		{id: "pikpak", kind: "pikpak", want: ""},
		{id: "guangyapan", kind: "guangyapan", want: ""},
		{id: "onedrive", kind: "onedrive", want: "root"},
		{id: "googledrive", kind: "googledrive", want: "root"},
		{id: "webdav", kind: "webdav", want: "/"},
		{id: "localstorage", kind: "localstorage", want: "/"},
		{id: "scriptcrawler", kind: "scriptcrawler", want: "/"},
	}
	for _, tc := range cases {
		if err := cat.UpsertDrive(ctx, &Drive{
			ID:   tc.id,
			Kind: tc.kind,
			Name: tc.kind,
		}); err != nil {
			t.Fatalf("upsert %s: %v", tc.kind, err)
		}
		got, err := cat.GetDrive(ctx, tc.id)
		if err != nil {
			t.Fatalf("get %s: %v", tc.kind, err)
		}
		if got.RootID != tc.want {
			t.Fatalf("%s rootId = %q, want %q", tc.kind, got.RootID, tc.want)
		}
		if got.ScanRootID != tc.want {
			t.Fatalf("%s scanRootId = %q, want %q", tc.kind, got.ScanRootID, tc.want)
		}
	}
}

func TestUpsertDriveIgnoresRootIDForLocalStorageAndScriptCrawler(t *testing.T) {
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

	for _, tc := range []struct {
		id   string
		kind string
	}{
		{id: "localstorage", kind: "localstorage"},
		{id: "scriptcrawler", kind: "scriptcrawler"},
	} {
		if err := cat.UpsertDrive(ctx, &Drive{
			ID:         tc.id,
			Kind:       tc.kind,
			Name:       tc.kind,
			RootID:     "manual-root",
			ScanRootID: "manual-scan-root",
		}); err != nil {
			t.Fatalf("upsert %s: %v", tc.kind, err)
		}
		got, err := cat.GetDrive(ctx, tc.id)
		if err != nil {
			t.Fatalf("get %s: %v", tc.kind, err)
		}
		if got.RootID != "/" {
			t.Fatalf("%s rootId = %q, want /", tc.kind, got.RootID)
		}
		if got.ScanRootID != "/" {
			t.Fatalf("%s scanRootId = %q, want /", tc.kind, got.ScanRootID)
		}
	}
}

func TestSetDriveRuntimeStatusTracksPlaybackFailureAndRecovery(t *testing.T) {
	ctx := context.Background()
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	drive := &Drive{
		ID:     "drive",
		Kind:   "p115",
		Name:   "115",
		Status: "ok",
		Credentials: map[string]string{
			"cookie": "credential-must-be-preserved",
		},
	}
	if err := cat.UpsertDrive(ctx, drive); err != nil {
		t.Fatalf("upsert drive: %v", err)
	}
	if err := cat.SetDriveRuntimeStatus(ctx, drive.ID, "error", "user not login"); err != nil {
		t.Fatalf("set error status: %v", err)
	}

	got, err := cat.GetDrive(ctx, drive.ID)
	if err != nil {
		t.Fatalf("get failed drive: %v", err)
	}
	if got.Status != "error" || !strings.Contains(got.LastError, "not login") {
		t.Fatalf("status=%q lastError=%q, want playback error", got.Status, got.LastError)
	}
	if got.Credentials["cookie"] != "credential-must-be-preserved" {
		t.Fatalf("credentials changed: %#v", got.Credentials)
	}

	if err := cat.SetDriveRuntimeStatus(ctx, drive.ID, "ok", ""); err != nil {
		t.Fatalf("recover status: %v", err)
	}
	got, err = cat.GetDrive(ctx, drive.ID)
	if err != nil {
		t.Fatalf("get recovered drive: %v", err)
	}
	if got.Status != "ok" || got.LastError != "" {
		t.Fatalf("status=%q lastError=%q, want recovered drive", got.Status, got.LastError)
	}
}

func TestSetDriveRuntimeStatusRejectsInvalidState(t *testing.T) {
	cat, err := Open(t.TempDir() + "/catalog.db")
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	if err := cat.SetDriveRuntimeStatus(context.Background(), "drive", "pending", ""); err == nil {
		t.Fatal("invalid runtime status unexpectedly accepted")
	}
}
