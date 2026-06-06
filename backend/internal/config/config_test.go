package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequiresAdminSetup(t *testing.T) {
	if !RequiresAdminSetup(&Config{Server: Server{Admin: Admin{Username: DefaultAdminUsername, Password: DefaultAdminPassword}}}) {
		t.Fatal("default admin credentials should require setup")
	}
	if RequiresAdminSetup(&Config{Server: Server{Admin: Admin{Username: "owner", Password: "secret123"}}}) {
		t.Fatal("custom admin credentials should not require setup")
	}
}

func TestWriteAdminCredentialsUpdatesConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  listen: "127.0.0.1:9192"
  admin:
    username: "admin"
    password: "admin123"
storage:
  db_path: "./data/video-site.db"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := WriteAdminCredentials(path, "owner", "new-secret"); err != nil {
		t.Fatalf("write admin credentials: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Server.Admin.Username != "owner" {
		t.Fatalf("username = %q, want owner", cfg.Server.Admin.Username)
	}
	if cfg.Server.Admin.Password != "new-secret" {
		t.Fatalf("password = %q, want new-secret", cfg.Server.Admin.Password)
	}
	if cfg.Server.Listen != "127.0.0.1:9192" {
		t.Fatalf("listen = %q, want preserved value", cfg.Server.Listen)
	}
	if cfg.Storage.DBPath != "./data/video-site.db" {
		t.Fatalf("db path = %q, want preserved value", cfg.Storage.DBPath)
	}
}
