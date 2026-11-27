package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

const (
	TabPayslips = 0
	TabPTO      = 1
	TabTrends   = 2
)

// Platform abstracts desktop OS actions so view tests and controller tests run headlessly.
type Platform interface {
	OpenPath(path string) error
	ShowInFolder(path string) error
	ChooseDirectory(title, current string) (string, error)
	CopyText(text string)
	Notify(id, title, body string)
	Preview(path string) error
	OpenAtLogin() bool
	SetOpenAtLogin(enabled bool) error
	Quit()
}

// App holds all UI and runtime state for the Zucchetti menu bar app.
// All fields are read and mutated on the main thread (via mygo.RunOnMain from background work).
type App struct {
	mu sync.Mutex // guards panel pointer for background goroutines

	tray           *mygo.Tray
	panel          *mygo.Window
	quitting       bool
	stopBackground func()

	// Dependencies (swappable in tests)
	configPath string
	cachePath  string
	secrets    SecretStore
	platform   Platform
	httpClient *http.Client
	runOnMain  func(func())

	// Persisted settings + Keychain token
	cfg   Config
	token string
	// Zucchetti portal login, from the Keychain.
	portalUser, portalPass string
	portalInst             Portal
	newPortal              func(user, pass string) Portal

	// Navigation
	tab          int // TabPayslips or TabPTO
	settingsOpen bool

	// Payslips state
	payslips      []Payslip
	downloaded    map[string]bool        // key -> exists in cfg.DownloadDir
	downloading   map[string]bool        // key -> download in progress
	refetching    map[string]bool        // key -> re-fetch from Zucchetti in progress
	details       map[string]PayslipData // key -> what was read from the saved PDF
	detailsBusy   bool
	loading       bool
	hasLoaded     bool
	cacheLoaded   bool
	errMsg        string
	noticeMsg     string
	noticeIsError bool
	noticeUntil   time.Time

	// Active workflow run state (POST /run -> GET /status/<id>)
	runningFetch bool
	fetchStatus  string // e.g. "queued", "running"

	// PTO request draft.
	ptoDate           time.Time
	ptoFrom           time.Time
	ptoTo             time.Time
	ptoJustification  string
	ptoMotivation     string
	ptoFullDay        bool
	ptoRequestOpen    bool
	ptoReview         bool
	ptoSubmitting     bool
	ptoRequestMessage string

	// Inline settings draft fields
	draftSource     int // 0: portal, 1: worker
	draftUser       string
	draftPass       string
	draftURL        string
	draftToken      string
	draftDir        string
	showToken       bool
	launchAtLogin   bool
	testingConn     bool
	testConnResult  string
	testConnIsError bool
	settingsDirty   bool

	// Optional hook overrides for headless view tests (if non-nil, invoked instead of real async methods)
	onSubmitPTO      func(PTORequest)
	onRefresh        func()
	onDownload       func(p Payslip)
	onDownloadAll    func()
	onTriggerFetch   func()
	onChooseFolder   func(forSettings bool)
	onTestConnection func()
	onSaveSettings   func()
	onOpenPayslip    func(p Payslip)
	onQuickLook      func(p Payslip)
	onRefetch        func(p Payslip)
	onRevealPayslip  func(p Payslip)
	onRevealFolder   func()
}

// NewApp creates an App with initialized maps and defaults.
func NewApp(cfgPath string, secrets SecretStore, platform Platform) *App {
	return &App{
		configPath:  cfgPath,
		secrets:     secrets,
		platform:    platform,
		downloaded:  make(map[string]bool),
		downloading: make(map[string]bool),
		refetching:  make(map[string]bool),
		details:     make(map[string]PayslipData),
		ptoDate:     time.Now(),
		runOnMain:   mygo.RunOnMain,
	}
}

// IsConfigured reports whether both WorkerURL and Token are present.
func (a *App) IsConfigured() bool {
	if a.usesPortal() {
		return a.portalUser != "" && a.portalPass != ""
	}
	return normalizeWorkerURL(a.cfg.WorkerURL) != "" && a.token != ""
}

// usesPortal reports whether payslips come from the Zucchetti portal. A
// config without a source is a worker's: that is what earlier versions wrote.
func (a *App) usesPortal() bool { return a.cfg.Source == SourcePortal }

// portal returns the portal client for the saved login, made once.
func (a *App) portal() Portal {
	if a.portalInst == nil {
		mk := a.newPortal
		if mk == nil {
			mk = newWebPortal
		}
		a.portalInst = mk(a.portalUser, a.portalPass)
	}
	return a.portalInst
}

// LocalPath returns where a payslip is saved in the configured folder:
// YYYY-MM.pdf. It is empty for a key that is not a payslip's.
func (a *App) LocalPath(key string) string {
	name, ok := LocalFileName(key)
	if a.cfg.DownloadDir == "" || !ok {
		return ""
	}
	return filepath.Join(a.cfg.DownloadDir, name)
}

// ScanLocalFiles updates a.downloaded by checking which payslips already exist in cfg.DownloadDir.
func (a *App) ScanLocalFiles() {
	if a.downloaded == nil {
		a.downloaded = make(map[string]bool)
	}
	clear(a.downloaded)
	if a.cfg.DownloadDir == "" {
		return
	}
	migrateLegacyNames(a.cfg.DownloadDir)
	for _, p := range a.payslips {
		path := a.LocalPath(p.Key)
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() > 0 {
			a.downloaded[p.Key] = true
		}
	}
}

// UndownloadedCount returns how many listed payslips are not yet saved locally.
func (a *App) UndownloadedCount() int {
	n := 0
	for _, p := range a.payslips {
		if !a.downloaded[p.Key] {
			n++
		}
	}
	return n
}

// DownloadedCount returns how many listed payslips are already saved locally.
func (a *App) DownloadedCount() int {
	return len(a.payslips) - a.UndownloadedCount()
}

// OpenSettings populates draft fields from current config and shows the inline settings panel.
func (a *App) OpenSettings() {
	a.draftSource = 0
	if !a.usesPortal() {
		a.draftSource = 1
	}
	a.draftUser = a.portalUser
	a.draftPass = a.portalPass
	a.draftURL = a.cfg.WorkerURL
	a.draftToken = a.token
	a.draftDir = a.cfg.DownloadDir
	a.showToken = false
	a.testConnResult = ""
	a.testConnIsError = false
	a.settingsDirty = false
	if a.platform != nil {
		a.launchAtLogin = a.platform.OpenAtLogin()
	}
	a.settingsOpen = true
}

// CloseSettings hides the inline settings panel.
func (a *App) CloseSettings() {
	a.settingsOpen = false
	a.showToken = false
}

// SetNotice displays a short inline status notice for 4 seconds.
func (a *App) SetNotice(msg string, isErr bool) {
	a.noticeMsg = msg
	a.noticeIsError = isErr
	a.noticeUntil = time.Now().Add(4 * time.Second)
}

// Invalidate schedules a redraw if the popover window is open.
func (a *App) Invalidate() {
	a.mu.Lock()
	p := a.panel
	a.mu.Unlock()
	if p != nil {
		p.Invalidate()
	}
}

func (a *App) clientFor(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    a.httpClient,
	}
}
