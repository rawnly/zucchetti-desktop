package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestParsePortalRows(t *testing.T) {
	// Row texts as the portal gives them, with invented ids.
	rows := []string{
		"Libro unico di 10-2026 Mensilità normale (XXXXXX00X00X000X-0000001)Libro unico da consegnare\\202627 Nov 2026 alle 00:31",
		"Certificazione Unica redditi 2025 (XXXXXX00X00X000X-0000001)Certificazione Unica\\202513 Mar 2026 alle 11:26",
		"Libro unico di 11-2026 Mensilità normale (XXXXXX00X00X000X-0000001)Libro unico da consegnare\\202627 Nov 2026 alle 14:50",
		"Libro unico di 12-2025 Mensilità normale (XXXXXX00X00X000X-0000001)Libro unico da consegnare\\20251 Dic 2025 alle 09:05",
		"Libro unico di 11-2026 Mensilità aggiuntiva (XXXXXX00X00X000X-0000001)Libro unico da consegnare\\202628 Nov 2026 alle 10:00", // a second row for a month: ignored
		"Libro unico di 13-2026 Mensilità normale",                                                         // not a month
		"Libro unico di 05-2025 Mensilità normale (X)Libro unico\\2025",                                    // no date
		"Libro unico di 07-2026 Mensilità normale (X)Libro unico da consegnare\\20264 Lug 2026 alle 19:02", // the folder year runs into the day
	}
	got := parsePortalRows(rows)
	var keys []string
	for _, p := range got {
		keys = append(keys, p.Key)
	}
	want := []string{"busta-2026-11.pdf", "busta-2026-10.pdf", "busta-2026-07.pdf", "busta-2025-12.pdf", "busta-2025-05.pdf"}
	if len(keys) != len(want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys %v, want %v", keys, want)
		}
	}
	if got[0].Year != 2026 || got[0].Month != 11 {
		t.Errorf("year/month %+v", got[0])
	}
	u := got[0].Uploaded
	if u.Day() != 27 || u.Month() != 11 || u.Hour() != 14 || u.Minute() != 50 {
		t.Errorf("uploaded %v", u)
	}
	if d := got[2].Uploaded; d.Day() != 4 || d.Month() != 7 || d.Year() != 2026 {
		t.Errorf("single-digit day after the folder year: %v", d)
	}
	if d := got[3].Uploaded; d.Day() != 1 || d.Month() != 12 || d.Year() != 2025 {
		t.Errorf("single-digit day: %v", d)
	}
	if !got[4].Uploaded.IsZero() {
		t.Errorf("a row without a date got %v", got[4].Uploaded)
	}
	if got[0].Size != 0 {
		t.Errorf("the portal gives no size, got %d", got[0].Size)
	}
}

// fakePortal serves a fixed list and writes small PDFs, like webPortal would.
type fakePortal struct {
	mu      sync.Mutex
	items   []Payslip
	listErr error
	failOn  map[string]error
	lists   int
	fetched []string
}

func (f *fakePortal) List(ctx context.Context) ([]Payslip, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	return f.items, f.listErr
}

func (f *fakePortal) Fetch(ctx context.Context, items []Payslip, dir string, each func(Payslip, error)) error {
	for _, p := range items {
		f.mu.Lock()
		f.fetched = append(f.fetched, p.Key)
		err := f.failOn[p.Key]
		f.mu.Unlock()
		if err == nil {
			name, _ := LocalFileName(p.Key)
			_, err = writeFileAtomic(dir, name, []byte("%PDF-1.4 "+p.Key))
		}
		each(p, err)
	}
	return nil
}

func portalApp(t *testing.T, f *fakePortal) (*App, *sync.Mutex) {
	t.Helper()
	app := newTestApp(t)
	var main sync.Mutex // stands in for the main thread
	app.runOnMain = func(fn func()) { main.Lock(); defer main.Unlock(); fn() }
	app.cfg = Config{Source: SourcePortal, DownloadDir: t.TempDir()}
	app.portalUser, app.portalPass = "u", "p"
	app.newPortal = func(user, pass string) Portal { return f }
	return app, &main
}

func waitFor(t *testing.T, main *sync.Mutex, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		main.Lock()
		ok := cond()
		main.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func samplePayslips() []Payslip {
	return []Payslip{
		{Key: "busta-2026-11.pdf", Year: 2026, Month: 11},
		{Key: "busta-2026-10.pdf", Year: 2026, Month: 10},
		{Key: "busta-2025-12.pdf", Year: 2025, Month: 12},
	}
}

func TestPortalIsConfiguredByLogin(t *testing.T) {
	app := newTestApp(t)
	app.cfg = Config{Source: SourcePortal}
	if app.IsConfigured() {
		t.Error("configured without a login")
	}
	app.portalUser = "u"
	if app.IsConfigured() {
		t.Error("configured without a password")
	}
	app.portalPass = "p"
	if !app.IsConfigured() {
		t.Error("not configured with a login")
	}
	app.cfg = Config{Source: SourceWorker, WorkerURL: "https://x.example", DownloadDir: "d"}
	if app.IsConfigured() {
		t.Error("worker source is configured by a login")
	}
}

func TestPortalRefreshAndDownloadAll(t *testing.T) {
	f := &fakePortal{items: samplePayslips()}
	app, main := portalApp(t, f)
	main.Lock()
	app.Refresh()
	main.Unlock()
	waitFor(t, main, "list", func() bool { return app.hasLoaded && len(app.payslips) == 3 })

	main.Lock()
	app.DownloadAll()
	main.Unlock()
	waitFor(t, main, "downloads", func() bool {
		return len(app.downloading) == 0 && app.DownloadedCount() == 3 && app.noticeMsg == "Saved 3 of 3 payslips"
	})
	for _, name := range []string{"2026-11.pdf", "2026-10.pdf", "2025-12.pdf"} {
		if _, err := os.Stat(filepath.Join(app.cfg.DownloadDir, name)); err != nil {
			t.Errorf("%s not saved: %v", name, err)
		}
	}
}

func TestPortalDownloadReportsAFailure(t *testing.T) {
	f := &fakePortal{items: samplePayslips(), failOn: map[string]error{"busta-2026-10.pdf": errors.New("the portal did not open Libro unico di 10-2026")}}
	app, main := portalApp(t, f)
	app.payslips = samplePayslips()
	main.Lock()
	app.DownloadAll()
	main.Unlock()
	waitFor(t, main, "summary", func() bool { return len(app.downloading) == 0 && app.noticeMsg != "" })
	main.Lock()
	defer main.Unlock()
	if !app.noticeIsError || app.noticeMsg != "Saved 2 of 3 payslips · the portal did not open Libro unico di 10-2026" {
		t.Errorf("notice %q error=%v", app.noticeMsg, app.noticeIsError)
	}
	if app.downloaded["busta-2026-10.pdf"] || !app.downloaded["busta-2026-11.pdf"] {
		t.Errorf("downloaded %v", app.downloaded)
	}
}

func TestPortalRefetchOverwrites(t *testing.T) {
	f := &fakePortal{items: samplePayslips()}
	app, main := portalApp(t, f)
	p := samplePayslips()[0]
	app.payslips = samplePayslips()
	old := filepath.Join(app.cfg.DownloadDir, "2026-11.pdf")
	os.WriteFile(old, []byte("%PDF-1.4 stale"), 0o600)
	main.Lock()
	app.Refetch(p)
	if !app.refetching[p.Key] {
		t.Error("no refetching flag")
	}
	main.Unlock()
	waitFor(t, main, "refetch", func() bool { return len(app.refetching) == 0 && app.noticeMsg != "" })
	b, _ := os.ReadFile(old)
	if string(b) != "%PDF-1.4 busta-2026-11.pdf" {
		t.Errorf("file %q", b)
	}
}

func TestPortalFetchLatest(t *testing.T) {
	f := &fakePortal{items: samplePayslips()}
	app, main := portalApp(t, f)
	main.Lock()
	app.TriggerFetch()
	if !app.runningFetch {
		t.Error("not running")
	}
	main.Unlock()
	waitFor(t, main, "latest saved", func() bool {
		return !app.runningFetch && app.downloaded["busta-2026-11.pdf"] && len(app.downloading) == 0
	})
	main.Lock()
	if len(f.fetched) != 1 || f.fetched[0] != "busta-2026-11.pdf" {
		t.Errorf("fetched %v", f.fetched)
	}
	app.TriggerFetch() // already saved: nothing more to fetch
	main.Unlock()
	waitFor(t, main, "up to date", func() bool { return !app.runningFetch && app.noticeMsg == "Up to date" })
	main.Lock()
	defer main.Unlock()
	if len(f.fetched) != 1 {
		t.Errorf("fetched again: %v", f.fetched)
	}
}

func TestPortalListError(t *testing.T) {
	f := &fakePortal{listErr: ErrPortalLogin}
	app, main := portalApp(t, f)
	main.Lock()
	app.Refresh()
	main.Unlock()
	waitFor(t, main, "error", func() bool { return app.errMsg != "" })
	main.Lock()
	defer main.Unlock()
	if app.errMsg != ErrPortalLogin.Error() || app.loading {
		t.Errorf("err %q loading %v", app.errMsg, app.loading)
	}
}

func TestSaveSettingsStoresPortalLogin(t *testing.T) {
	f := &fakePortal{items: samplePayslips()}
	app, main := portalApp(t, f)
	secrets := &fakeSecrets{}
	app.secrets = secrets
	app.configPath = filepath.Join(t.TempDir(), "config.json")
	app.portalUser, app.portalPass = "", ""
	app.cfg = Config{DownloadDir: app.cfg.DownloadDir}
	main.Lock()
	app.OpenSettings()
	app.draftSource = 0
	app.draftUser, app.draftPass = "me", "secret"
	app.SaveSettings()
	main.Unlock()
	main.Lock()
	defer main.Unlock()
	if secrets.vals[userSecretKey] != "me" || secrets.vals[passSecretKey] != "secret" {
		t.Errorf("keychain %v", secrets.vals)
	}
	if !app.usesPortal() || !app.IsConfigured() {
		t.Error("not using the portal after saving")
	}
	cfg, err := loadConfig(app.configPath)
	if err != nil || cfg.Source != SourcePortal {
		t.Errorf("config %+v %v", cfg, err)
	}
	if data, _ := os.ReadFile(app.configPath); len(data) > 0 && (contains(string(data), "secret") || contains(string(data), `"me"`)) {
		t.Errorf("login leaked into config.json: %s", data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLoadConfigDefaultsTheSource(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.json")
		os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	if c, _ := loadConfig(filepath.Join(dir, "none.json")); c.Source != SourcePortal {
		t.Errorf("fresh config: %q", c.Source)
	}
	if c, _ := loadConfig(write(`{"workerUrl":"https://w.example"}`)); c.Source != SourceWorker {
		t.Errorf("config of a worker user: %q", c.Source)
	}
	if c, _ := loadConfig(write(`{"source":"portal","workerUrl":"https://w.example"}`)); c.Source != SourcePortal {
		t.Errorf("explicit source: %q", c.Source)
	}
}

func TestSettingsSwitchesSource(t *testing.T) {
	app := newTestApp(t)
	app.cfg = Config{Source: SourcePortal, DownloadDir: t.TempDir()}
	app.OpenSettings()
	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()
	for _, want := range []string{"Username", "Password"} {
		if !tt.HasText(want) {
			t.Errorf("portal fields: missing %q in %q", want, tt.Texts())
		}
	}
	if tt.HasText("Worker URL") {
		t.Error("worker fields shown for the portal")
	}
	if err := tt.Click("Worker"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.draftSource != 1 || !tt.HasText("Worker URL") || !tt.HasText("Access token") || tt.HasText("Username") {
		t.Errorf("after switching: source %d, %q", app.draftSource, tt.Texts())
	}
}
