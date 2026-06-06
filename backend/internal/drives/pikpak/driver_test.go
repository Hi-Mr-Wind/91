package pikpak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewDefaults(t *testing.T) {
	d := New(Config{
		ID:       "pikpak-main",
		Username: "user@example.com",
		Password: "secret",
		RootID:   "0",
	})

	if d.Kind() != "pikpak" {
		t.Fatalf("kind = %q, want pikpak", d.Kind())
	}
	if d.ID() != "pikpak-main" {
		t.Fatalf("id = %q, want pikpak-main", d.ID())
	}
	if d.RootID() != "" {
		t.Fatalf("root id = %q, want empty PikPak root", d.RootID())
	}
	if d.platform != "web" {
		t.Fatalf("platform = %q, want web", d.platform)
	}
	if d.deviceID == "" {
		t.Fatal("device id should be generated")
	}
	if d.userAgent == "" {
		t.Fatal("user agent should be selected")
	}
}

func TestFileToEntry(t *testing.T) {
	mod := time.Date(2026, 5, 10, 12, 30, 0, 0, time.UTC)
	f := file{
		ID:            "file-id",
		Name:          "movie.mp4",
		Kind:          "drive#file",
		Hash:          "hash-value",
		Size:          "12345",
		ThumbnailLink: "https://thumbnail.example/movie.jpg",
		ModifiedTime:  mod,
	}

	got := fileToEntry(f, "parent-id")

	if got.ID != "file-id" {
		t.Fatalf("id = %q, want file-id", got.ID)
	}
	if got.Name != "movie.mp4" {
		t.Fatalf("name = %q, want movie.mp4", got.Name)
	}
	if got.IsDir {
		t.Fatal("file should not be a directory")
	}
	if got.Size != 12345 {
		t.Fatalf("size = %d, want 12345", got.Size)
	}
	if got.ParentID != "parent-id" {
		t.Fatalf("parent id = %q, want parent-id", got.ParentID)
	}
	if got.MimeType != "video/mp4" {
		t.Fatalf("mime = %q, want video/mp4", got.MimeType)
	}
	if got.ThumbnailURL != "https://thumbnail.example/movie.jpg" {
		t.Fatalf("thumbnail = %q, want remote thumbnail", got.ThumbnailURL)
	}
	if got.Hash != "hash-value" {
		t.Fatalf("hash = %q, want hash-value", got.Hash)
	}
	if !got.ModTime.Equal(mod) {
		t.Fatalf("mod time = %v, want %v", got.ModTime, mod)
	}
}

func TestFolderToEntry(t *testing.T) {
	f := file{
		ID:   "folder-id",
		Name: "Videos",
		Kind: "drive#folder",
	}

	got := fileToEntry(f, "")

	if !got.IsDir {
		t.Fatal("folder should be a directory")
	}
	if got.Size != 0 {
		t.Fatalf("size = %d, want 0", got.Size)
	}
}

func TestEnsureDirReusesExistingFolder(t *testing.T) {
	var postCalled bool
	mux := http.NewServeMux()
	mux.HandleFunc("/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if got := r.URL.Query().Get("parent_id"); got != "root-id" {
				t.Fatalf("parent_id = %q, want root-id", got)
			}
			writePikPakJSON(t, w, map[string]any{
				"files": []map[string]any{{
					"id":   "existing-folder-id",
					"kind": "drive#folder",
					"name": "91 Spider",
				}},
			})
		case http.MethodPost:
			postCalled = true
			t.Fatalf("existing folder should not be created again")
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d := newTestDriver(t, srv)
	got, err := d.EnsureDir(context.Background(), "91 Spider")
	if err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	if got != "existing-folder-id" {
		t.Fatalf("dir id = %q, want existing-folder-id", got)
	}
	if postCalled {
		t.Fatal("POST should not be called")
	}
}

func TestEnsureDirCreatesMissingFolder(t *testing.T) {
	var got uploadRequestBody
	mux := http.NewServeMux()
	mux.HandleFunc("/drive/v1/files", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writePikPakJSON(t, w, map[string]any{"files": []map[string]any{}})
		case http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode create folder body: %v", err)
			}
			writePikPakJSON(t, w, map[string]any{
				"id":   "new-folder-id",
				"kind": "drive#folder",
				"name": "91 Spider",
			})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d := newTestDriver(t, srv)
	id, err := d.EnsureDir(context.Background(), "91 Spider")
	if err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	if id != "new-folder-id" {
		t.Fatalf("dir id = %q, want new-folder-id", id)
	}
	if got.Kind != "drive#folder" || got.ParentID != "root-id" || got.Name != "91 Spider" {
		t.Fatalf("create folder body = %#v", got)
	}
}

func writePikPakJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("write json: %v", err)
	}
}
