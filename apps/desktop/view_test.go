package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type fakePlatform struct {
	openedPaths   []string
	shownPaths    []string
	previewed     []string
	chosenDirs    []string
	copiedTexts   []string
	notifications []string
	openAtLogin   bool
	quitCalled    bool
}

func (f *fakePlatform) OpenPath(path string) error {
	f.openedPaths = append(f.openedPaths, path)
	return nil
}

func (f *fakePlatform) ShowInFolder(path string) error {
	f.shownPaths = append(f.shownPaths, path)
	return nil
}

func (f *fakePlatform) ChooseDirectory(title, current string) (string, error) {
	f.chosenDirs = append(f.chosenDirs, title)
	return "", nil
}

func (f *fakePlatform) CopyText(text string) {
	f.copiedTexts = append(f.copiedTexts, text)
}

func (f *fakePlatform) Notify(id, title, body string) {
	f.notifications = append(f.notifications, title+": "+body)
}

func (f *fakePlatform) Preview(path string) error {
	f.previewed = append(f.previewed, path)
	return nil
}

func (f *fakePlatform) OpenAtLogin() bool { return f.openAtLogin }

func (f *fakePlatform) SetOpenAtLogin(enabled bool) error {
	f.openAtLogin = enabled
	return nil
}

func (f *fakePlatform) Quit() { f.quitCalled = true }

type fakeSecrets struct{ vals map[string]string }

func (f *fakeSecrets) Get(key string) (string, error) { return f.vals[key], nil }
func (f *fakeSecrets) Set(key, value string) error {
	if f.vals == nil {
		f.vals = make(map[string]string)
	}
	f.vals[key] = value
	return nil
}
func (f *fakeSecrets) Delete(key string) error {
	delete(f.vals, key)
	return nil
}

func newTestApp(tb testing.TB) *App {
	tb.Helper()
	app := NewApp("", &fakeSecrets{}, &fakePlatform{})
	return app
}

func TestUnconfiguredFlow(t *testing.T) {
	app := newTestApp(t)
	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()

	if !tt.HasText("Connect to buste-paga-cf") {
		t.Fatal("expected unconfigured empty state")
	}
	if err := tt.Click("Configure connection"); err != nil {
		t.Fatalf("click Configure connection: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Worker URL") {
		t.Fatal("expected settings panel to open")
	}
	if !tt.HasText("Access token") {
		t.Fatal("expected access token field")
	}
}

func TestPopulatedPayslips(t *testing.T) {
	app := newTestApp(t)
	app.cfg.WorkerURL = "https://example.com"
	app.token = "token"
	app.hasLoaded = true
	app.payslips = []Payslip{
		{Key: "busta-2026-11.pdf", Year: 2026, Month: 11, Size: 142000, Uploaded: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)},
		{Key: "busta-2026-10.pdf", Year: 2026, Month: 10, Size: 138000, Uploaded: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
	}

	var downloadedKey string
	var downloadedAll, fetched bool
	app.onDownload = func(p Payslip) { downloadedKey = p.Key }
	app.onDownloadAll = func() { downloadedAll = true }
	app.onTriggerFetch = func() { fetched = true }

	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()

	if !tt.HasText("November") {
		t.Fatal("expected newest payslip title")
	}
	if !tt.HasText("Download all (2)") {
		t.Fatal("expected Download all button with count 2")
	}

	if err := tt.Click("Download November 2026"); err != nil {
		t.Fatalf("click download row: %v", err)
	}
	tt.Frame()
	if downloadedKey != "busta-2026-11.pdf" {
		t.Fatalf("expected download of newest payslip, got %q", downloadedKey)
	}

	if err := tt.Click("Download all (2)"); err != nil {
		t.Fatalf("click download all: %v", err)
	}
	tt.Frame()
	if !downloadedAll {
		t.Fatal("expected Download all callback")
	}

	if err := tt.Click("Fetch latest"); err != nil {
		t.Fatalf("click fetch latest: %v", err)
	}
	tt.Frame()
	if !fetched {
		t.Fatal("expected Fetch latest callback")
	}
}

func TestPTOTab(t *testing.T) {
	app := newTestApp(t)
	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()

	if err := tt.Click("PTO"); err != nil {
		t.Fatalf("click PTO tab: %v", err)
	}
	tt.Frame()
	if app.tab != TabPTO {
		t.Fatalf("expected tab %d, got %d", TabPTO, app.tab)
	}
	if !tt.HasText("Requests soon") {
		t.Fatal("expected PTO coming soon pill")
	}
	if !tt.HasText("Request PTO") {
		t.Fatal("expected Request PTO button")
	}
}

func TestSettingsEditSave(t *testing.T) {
	app := newTestApp(t)
	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()

	if err := tt.Click("Configure connection"); err != nil {
		t.Fatalf("click configure: %v", err)
	}
	tt.Frame()

	if err := tt.Click("Worker URL input"); err != nil {
		t.Fatalf("focus worker url input: %v", err)
	}
	tt.Frame()
	tt.Type("https://edited.example.com")
	tt.Frame()

	if app.draftURL != "https://edited.example.com" {
		t.Fatalf("expected draftURL %q, got %q", "https://edited.example.com", app.draftURL)
	}

	var saved bool
	app.onSaveSettings = func() { saved = true }
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("click save: %v", err)
	}
	tt.Frame()
	if !saved {
		t.Fatal("expected Save callback")
	}
}

func TestSnapshot(t *testing.T) {
	app := newTestApp(t)
	app.cfg.WorkerURL = "https://example.com"
	app.token = "token"
	app.hasLoaded = true
	app.payslips = []Payslip{
		{Key: "busta-2026-11.pdf", Year: 2026, Month: 11, Size: 142000, Uploaded: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)},
	}

	tt := ui.NewTester(app.view, 368, 488)
	tt.Frame()

	img := tt.Image()
	if img == nil {
		t.Fatal("expected snapshot image")
	}
	if img.Bounds().Empty() {
		t.Fatal("expected non-empty snapshot image")
	}
}
