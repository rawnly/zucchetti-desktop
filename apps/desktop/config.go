package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

const (
	configFileName = "config.json"
	tokenSecretKey = "worker-run-token"
	userSecretKey  = "zucchetti-user"
	passSecretKey  = "zucchetti-password"

	// SourcePortal reads payslips from the Zucchetti portal itself;
	// SourceWorker from a deployed buste-paga worker.
	SourcePortal = "portal"
	SourceWorker = "worker"
)

// Config holds non-secret app preferences stored in PathUserData/config.json.
// The worker token is stored separately in the OS credential store (Keychain on macOS).
type Config struct {
	Source      string `json:"source"`
	WorkerURL   string `json:"workerUrl"`
	DownloadDir string `json:"downloadDir"`
}

// SecretStore abstracts OS keychain access so tests stay isolated.
type SecretStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

type mygoSecrets struct{}

func (mygoSecrets) Get(key string) (string, error) {
	b, err := mygo.Secrets.Get(key)
	if errors.Is(err, mygo.ErrSecretNotFound) || errors.Is(err, errors.ErrUnsupported) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (mygoSecrets) Set(key, value string) error {
	if value == "" {
		return mygo.Secrets.Delete(key)
	}
	return mygo.Secrets.Set(key, []byte(value))
}

func (mygoSecrets) Delete(key string) error {
	return mygo.Secrets.Delete(key)
}

func defaultDownloadDir() string {
	if docs, err := mygo.App.Path(mygo.PathDocuments); err == nil && docs != "" {
		return filepath.Join(docs, "Payslips")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Documents", "Payslips")
	}
	return "Payslips"
}

func configPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func loadConfig(path string) (Config, error) {
	cfg := Config{DownloadDir: defaultDownloadDir()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg.Source = SourcePortal // a fresh install
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if strings.TrimSpace(cfg.DownloadDir) == "" {
		cfg.DownloadDir = defaultDownloadDir()
	}
	cfg.WorkerURL = normalizeWorkerURL(cfg.WorkerURL)
	if cfg.Source == "" {
		// A config from before the portal source had a worker, or nothing.
		cfg.Source = SourcePortal
		if cfg.WorkerURL != "" {
			cfg.Source = SourceWorker
		}
	}
	return cfg, nil
}

func payslipCachePath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "payslips-cache.json")
}

func loadPayslipCache(path string) ([]Payslip, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []Payslip
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	for _, p := range items {
		if !IsValidBustaKey(p.Key) {
			return nil, errors.New("invalid payslip cache")
		}
	}
	return items, nil
}

func savePayslipCache(path string, items []Payslip) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cfg.WorkerURL = normalizeWorkerURL(cfg.WorkerURL)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeWorkerURL(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimRight(s, "/")
	if s != "" && !strings.Contains(s, "://") {
		scheme := "https://"
		host := strings.SplitN(strings.SplitN(s, "/", 2)[0], ":", 2)[0]
		if host == "localhost" || host == "127.0.0.1" || host == "[::1]" || strings.HasSuffix(host, ".localhost") {
			scheme = "http://"
		}
		s = scheme + s
	}
	return s
}

// shortenHome replaces the user's home directory prefix with ~ for display.
func shortenHome(p string) string {
	if p == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(p, prefix) {
		return "~" + string(os.PathSeparator) + strings.TrimPrefix(p, prefix)
	}
	return p
}

// migrateLegacyNames renames payslips saved as busta-YYYY-MM.pdf to
// YYYY-MM.pdf, which earlier versions of the app used. A file is never
// overwritten: if the new name exists, the old one stays.
func migrateLegacyNames(dir string) {
	old, err := filepath.Glob(filepath.Join(dir, "busta-????-??.pdf"))
	if err != nil {
		return
	}
	for _, path := range old {
		name, ok := LocalFileName(filepath.Base(path))
		if !ok {
			continue
		}
		target := filepath.Join(dir, name)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		_ = os.Rename(path, target)
	}
}
