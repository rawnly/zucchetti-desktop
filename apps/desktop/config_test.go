package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeWorkerURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"https://example.com////", "https://example.com"},
		{"  https://example.com/path/  ", "https://example.com/path"},
		{"http://example.com/path", "http://example.com/path"},
		{"example.com/worker", "https://example.com/worker"},
		{"example.com", "https://example.com"},
		{"localhost:8787", "http://localhost:8787"},
		{"127.0.0.1:8787/", "http://127.0.0.1:8787"},
		{"http://localhost:8787", "http://localhost:8787"},
	}
	for _, tt := range tests {
		if got := normalizeWorkerURL(tt.in); got != tt.want {
			t.Errorf("normalizeWorkerURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("missing file fallback", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.json")
		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig(missing) error = %v", err)
		}
		if cfg.WorkerURL != "" {
			t.Errorf("WorkerURL = %q, want empty", cfg.WorkerURL)
		}
		if cfg.DownloadDir == "" || !strings.HasSuffix(cfg.DownloadDir, "Payslips") {
			t.Errorf("DownloadDir = %q, want non-empty path ending in Payslips", cfg.DownloadDir)
		}
	})

	t.Run("valid json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"workerUrl":"example.com/worker/","downloadDir":"/tmp/payslips"}`), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig(valid) error = %v", err)
		}
		if cfg.WorkerURL != "https://example.com/worker" {
			t.Errorf("WorkerURL = %q, want https://example.com/worker", cfg.WorkerURL)
		}
		if cfg.DownloadDir != "/tmp/payslips" {
			t.Errorf("DownloadDir = %q, want /tmp/payslips", cfg.DownloadDir)
		}
	})

	t.Run("empty download dir fallback", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"workerUrl":"","downloadDir":"   "}`), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		cfg, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig() error = %v", err)
		}
		if cfg.WorkerURL != "" {
			t.Errorf("WorkerURL = %q, want empty", cfg.WorkerURL)
		}
		if cfg.DownloadDir == "" || !strings.HasSuffix(cfg.DownloadDir, "Payslips") {
			t.Errorf("DownloadDir = %q, want default ending in Payslips", cfg.DownloadDir)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"workerUrl":`), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		if _, err := loadConfig(path); err == nil {
			t.Errorf("loadConfig(bad json) error = nil, want non-nil")
		}
	})
}

func TestSaveConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg := Config{WorkerURL: "example.com/worker/", DownloadDir: "/tmp/dir"}
	if err := saveConfig(path, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	got, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if got.WorkerURL != "https://example.com/worker" {
		t.Errorf("round-trip WorkerURL = %q, want https://example.com/worker", got.WorkerURL)
	}
	if got.DownloadDir != "/tmp/dir" {
		t.Errorf("round-trip DownloadDir = %q, want /tmp/dir", got.DownloadDir)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("saved config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestShortenHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory available: %v", err)
	}
	if home == string(os.PathSeparator) {
		t.Skip("home directory is filesystem root, can't build a non-home path")
	}

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{home, "~"},
		{home + string(os.PathSeparator) + "Documents", "~" + string(os.PathSeparator) + "Documents"},
		{home + string(os.PathSeparator) + "Documents" + string(os.PathSeparator) + "Payslips", "~" + string(os.PathSeparator) + "Documents" + string(os.PathSeparator) + "Payslips"},
		{"relative/path", "relative/path"},
		{filepath.Join(os.TempDir(), "zucchetti-unrelated"), filepath.Join(os.TempDir(), "zucchetti-unrelated")},
	}
	for _, tt := range tests {
		if got := shortenHome(tt.in); got != tt.want {
			t.Errorf("shortenHome(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAppHelpers(t *testing.T) {
	dir := t.TempDir()
	a := NewApp(filepath.Join(dir, "config.json"), nil, nil)

	// IsConfigured
	a.cfg = Config{WorkerURL: "example.com", DownloadDir: dir}
	a.token = "tok"
	if !a.IsConfigured() {
		t.Errorf("IsConfigured() = false, want true with WorkerURL and token")
	}
	a.token = ""
	if a.IsConfigured() {
		t.Errorf("IsConfigured() = true, want false with empty token")
	}
	a.token = "tok"
	a.cfg.WorkerURL = "   "
	if a.IsConfigured() {
		t.Errorf("IsConfigured() = true, want false with blank WorkerURL")
	}
	a.cfg.WorkerURL = "example.com"

	// LocalPath
	if got := a.LocalPath("busta-2026-11.pdf"); got != filepath.Join(dir, "2026-11.pdf") {
		t.Errorf("LocalPath(valid) = %q, want %q", got, filepath.Join(dir, "2026-11.pdf"))
	}
	if got := a.LocalPath("../evil.pdf"); got != "" {
		t.Errorf("LocalPath(invalid) = %q, want empty", got)
	}
	a.cfg.DownloadDir = ""
	if got := a.LocalPath("busta-2026-11.pdf"); got != "" {
		t.Errorf("LocalPath(empty dir) = %q, want empty", got)
	}
	a.cfg.DownloadDir = dir

	// ScanLocalFiles and counts
	a.payslips = []Payslip{
		{Key: "busta-2026-11.pdf"},
		{Key: "busta-2026-10.pdf"},
		{Key: "busta-2026-09.pdf"},
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-11.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatalf("writing downloaded file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-10.pdf"), nil, 0o644); err != nil {
		t.Fatalf("writing empty file: %v", err)
	}

	a.downloaded = nil
	a.ScanLocalFiles()
	if a.downloaded == nil {
		t.Fatalf("ScanLocalFiles() did not initialize downloaded map")
	}
	if !a.downloaded["busta-2026-11.pdf"] {
		t.Errorf("downloaded[busta-2026-11.pdf] = false, want true")
	}
	if a.downloaded["busta-2026-10.pdf"] {
		t.Errorf("downloaded[busta-2026-10.pdf] = true for empty file, want false")
	}
	if a.downloaded["busta-2026-09.pdf"] {
		t.Errorf("downloaded[busta-2026-09.pdf] = true for missing file, want false")
	}
	if got := a.DownloadedCount(); got != 1 {
		t.Errorf("DownloadedCount() = %d, want 1", got)
	}
	if got := a.UndownloadedCount(); got != 2 {
		t.Errorf("UndownloadedCount() = %d, want 2", got)
	}

	// Empty download dir means nothing is downloaded.
	a.cfg.DownloadDir = ""
	a.ScanLocalFiles()
	if len(a.downloaded) != 0 {
		t.Errorf("downloaded map has %d entries with empty DownloadDir, want 0", len(a.downloaded))
	}
	if got := a.DownloadedCount(); got != 0 {
		t.Errorf("DownloadedCount() = %d with empty DownloadDir, want 0", got)
	}
	if got := a.UndownloadedCount(); got != 3 {
		t.Errorf("UndownloadedCount() = %d with empty DownloadDir, want 3", got)
	}
}
