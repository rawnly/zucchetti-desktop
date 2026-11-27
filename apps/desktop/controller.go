package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// dispatch runs fn on the main thread, where the app's state is read and
// changed. runOnMain is swapped out in headless tests, where fn runs inline.
func (a *App) dispatch(fn func()) {
	if a.runOnMain != nil {
		a.runOnMain(fn)
		return
	}
	fn()
}

// notify shows a desktop notification when a platform is wired up.
func (a *App) notify(id, title, body string) {
	if a.platform != nil {
		a.platform.Notify(id, title, body)
	}
}

// LoadInitialState reads the saved config and the keychain token, then loads
// the payslip list when the app is configured. Runs once on the main thread
// after the app is ready.
func (a *App) LoadInitialState() {
	cfg, err := loadConfig(a.configPath)
	if err != nil {
		a.errMsg = err.Error()
	} else {
		a.cfg = cfg
	}
	if a.secrets != nil {
		token, err := a.secrets.Get(tokenSecretKey)
		if err != nil {
			a.errMsg = err.Error()
		} else {
			a.token = token
		}
		a.portalUser, _ = a.secrets.Get(userSecretKey)
		a.portalPass, _ = a.secrets.Get(passSecretKey)
	}
	if a.platform != nil {
		a.launchAtLogin = a.platform.OpenAtLogin()
	}
	a.cachePath = payslipCachePath(a.configPath)
	if items, err := loadPayslipCache(a.cachePath); err == nil {
		a.payslips = items
		a.hasLoaded = true
		a.cacheLoaded = true
		a.ScanLocalFiles()
		a.loadDetails()
	}
	a.ScanLocalFiles()
	if a.IsConfigured() {
		a.Refresh()
	}
}

// Refresh reloads the payslip list from the worker in the background.
func (a *App) Refresh() {
	if a.onRefresh != nil {
		a.onRefresh()
		return
	}
	if !a.IsConfigured() || a.loading || a.quitting {
		return
	}
	a.loading = true
	a.errMsg = ""
	hadCache := a.cacheLoaded
	previous := make(map[string]bool, len(a.payslips))
	for _, p := range a.payslips {
		previous[p.Key] = true
	}
	a.Invalidate()
	list, limit := a.lister()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		defer cancel()
		items, err := list(ctx)
		a.dispatch(func() {
			a.loading = false
			a.hasLoaded = true
			if err != nil {
				a.errMsg = err.Error()
			} else {
				a.payslips = items
				if a.cachePath != "" {
					if err := savePayslipCache(a.cachePath, items); err != nil {
						a.errMsg = "Could not save payslip cache: " + err.Error()
					}
				}
				a.cacheLoaded = true
			}
			a.ScanLocalFiles()
			a.loadDetails()
			var added []Payslip
			if err == nil && hadCache {
				for _, p := range items {
					if !previous[p.Key] && !a.downloaded[p.Key] {
						added = append(added, p)
					}
				}
			}
			a.Invalidate()
			if len(added) > 0 {
				a.downloadNewPayslips(added)
			}
		})
	}()
}

// downloadNewPayslips downloads only entries newly added to a previously cached list.
func (a *App) downloadNewPayslips(items []Payslip) {
	a.notify("new-payslips", "New Zucchetti payslip", fmt.Sprintf("Downloading %d new payslip(s).", len(items)))
	if a.usesPortal() {
		a.portalSave(items, a.downloading, true)
		return
	}
	for _, p := range items {
		a.Download(p)
	}
}

// Download saves one payslip into the download folder in the background.
func (a *App) Download(p Payslip) {
	if a.onDownload != nil {
		a.onDownload(p)
		return
	}
	if !a.IsConfigured() || a.downloading[p.Key] || a.quitting {
		return
	}
	if a.usesPortal() {
		a.portalSave([]Payslip{p}, a.downloading, false)
		return
	}
	a.downloading[p.Key] = true
	a.Invalidate()
	client := a.clientFor(a.cfg.WorkerURL, a.token)
	key, dir := p.Key, a.cfg.DownloadDir
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err := client.DownloadPayslip(ctx, key, dir)
		a.dispatch(func() {
			delete(a.downloading, key)
			if err != nil {
				a.SetNotice(err.Error(), true)
			} else {
				a.downloaded[key] = true
				delete(a.details, key)
				a.SetNotice("Saved "+key, false)
				a.loadDetails()
			}
			a.Invalidate()
		})
	}()
}

// DownloadAll saves every payslip that is not in the download folder yet, at
// most three at a time, then reports how it went.
func (a *App) DownloadAll() {
	if a.onDownloadAll != nil {
		a.onDownloadAll()
		return
	}
	if !a.IsConfigured() || a.quitting {
		return
	}
	var targets []Payslip
	for _, p := range a.payslips {
		if !a.downloaded[p.Key] && !a.downloading[p.Key] {
			targets = append(targets, p)
		}
	}
	if len(targets) == 0 {
		return
	}
	if a.usesPortal() {
		a.portalSave(targets, a.downloading, true)
		return
	}
	for _, p := range targets {
		a.downloading[p.Key] = true
	}
	a.Invalidate()

	client := a.clientFor(a.cfg.WorkerURL, a.token)
	dir := a.cfg.DownloadDir
	jobs := make(chan Payslip)
	go func() {
		defer close(jobs)
		for _, p := range targets {
			jobs <- p
		}
	}()
	workers := min(3, len(targets))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				_, err := client.DownloadPayslip(ctx, p.Key, dir)
				cancel()
				key := p.Key
				a.dispatch(func() {
					delete(a.downloading, key)
					if err == nil {
						a.downloaded[key] = true
						delete(a.details, key)
					}
					a.Invalidate()
				})
			}
		}()
	}
	go func() {
		wg.Wait()
		a.dispatch(func() {
			saved := 0
			for _, p := range targets {
				if a.downloaded[p.Key] {
					saved++
				}
			}
			a.loadDetails()
			failed := len(targets) - saved
			msg := fmt.Sprintf("Saved %d of %d payslips", saved, len(targets))
			if failed > 0 {
				msg = fmt.Sprintf("Saved %d of %d payslips · %d failed", saved, len(targets), failed)
			}
			a.SetNotice(msg, failed > 0)
			a.Invalidate()
		})
	}()
}

// TriggerFetch asks the worker to scrape the current month, then follows the
// workflow run until it reaches a final state.
func (a *App) TriggerFetch() {
	if a.onTriggerFetch != nil {
		a.onTriggerFetch()
		return
	}
	if !a.IsConfigured() || a.runningFetch || a.quitting {
		return
	}
	if a.usesPortal() {
		a.portalFetchLatest()
		return
	}
	a.runningFetch = true
	a.fetchStatus = "queued"
	a.Invalidate()
	client := a.clientFor(a.cfg.WorkerURL, a.token)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		id, err := client.TriggerRun(ctx, "")
		if err != nil {
			a.dispatch(func() {
				a.runningFetch = false
				a.fetchStatus = ""
				a.SetNotice(err.Error(), true)
				a.Invalidate()
			})
			return
		}
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				a.dispatch(func() {
					a.runningFetch = false
					a.fetchStatus = ""
					a.SetNotice("Fetch timed out", true)
					a.notify("fetch", "Zucchetti", "Fetch timed out")
					a.Invalidate()
				})
				return
			case <-ticker.C:
				st, err := client.GetRunStatus(ctx, id)
				if err != nil {
					continue // a single poll may fail; keep following the run
				}
				terminal := st.IsTerminal()
				status := st.Status
				a.dispatch(func() {
					if a.fetchStatus != status {
						a.fetchStatus = status
					}
					if !terminal {
						a.Invalidate()
						return
					}
					a.runningFetch = false
					a.fetchStatus = ""
					if status == "complete" {
						a.notify("fetch", "Zucchetti", "Current month fetched")
						a.Refresh()
					} else {
						a.SetNotice("Fetch "+status, true)
						a.notify("fetch", "Zucchetti", "Fetch "+status)
					}
					a.Invalidate()
				})
				if terminal {
					return
				}
			}
		}
	}()
}

// ChooseFolder opens the system directory picker and applies the choice to
// the settings draft or to the saved download folder.
func (a *App) ChooseFolder(forSettings bool) {
	if a.onChooseFolder != nil {
		a.onChooseFolder(forSettings)
		return
	}
	if a.platform == nil || a.quitting {
		return
	}
	current := a.cfg.DownloadDir
	if forSettings {
		current = a.draftDir
	}
	platform := a.platform
	go func() {
		dir, err := platform.ChooseDirectory("Choose payslips folder", current)
		a.dispatch(func() {
			switch {
			case err != nil:
				a.SetNotice(err.Error(), true)
			case dir == "":
				// Canceled: nothing to apply.
			case forSettings:
				a.draftDir = dir
				a.settingsDirty = true
			default:
				a.cfg.DownloadDir = dir
				if err := saveConfig(a.configPath, a.cfg); err != nil {
					a.SetNotice(err.Error(), true)
				}
				a.ScanLocalFiles()
			}
			// The picker closes the popover: bring it back.
			a.showPanel()
			a.Invalidate()
		})
	}()
}

// TestConnection checks the draft URL and token against the worker.
func (a *App) TestConnection() {
	if a.onTestConnection != nil {
		a.onTestConnection()
		return
	}
	if a.testingConn || a.quitting {
		return
	}
	a.testingConn = true
	a.testConnResult = ""
	a.testConnIsError = false
	a.Invalidate()
	list, limit := func() (func(context.Context) ([]Payslip, error), time.Duration) {
		if a.draftSource == 0 {
			mk := a.newPortal
			if mk == nil {
				mk = newWebPortal
			}
			return mk(a.draftUser, a.draftPass).List, 3 * time.Minute
		}
		return a.clientFor(a.draftURL, a.draftToken).ListPayslips, 20 * time.Second
	}()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		defer cancel()
		items, err := list(ctx)
		a.dispatch(func() {
			a.testingConn = false
			if err != nil {
				a.testConnResult = err.Error()
				a.testConnIsError = true
			} else {
				a.testConnResult = fmt.Sprintf("Connected · %d payslips", len(items))
				a.testConnIsError = false
			}
			a.Invalidate()
		})
	}()
}

// SaveSettings applies the settings draft to the config file and the
// keychain, then reloads the payslip list.
func (a *App) SaveSettings() {
	if a.onSaveSettings != nil {
		a.onSaveSettings()
		return
	}
	cfg := Config{
		Source:      SourcePortal,
		WorkerURL:   normalizeWorkerURL(a.draftURL),
		DownloadDir: a.draftDir,
	}
	if a.draftSource == 1 {
		cfg.Source = SourceWorker
	}
	if cfg.DownloadDir == "" {
		cfg.DownloadDir = defaultDownloadDir()
	}
	if err := saveConfig(a.configPath, cfg); err != nil {
		a.SetNotice(err.Error(), true)
		a.Invalidate()
		return
	}
	if a.secrets != nil {
		if err := a.secrets.Set(tokenSecretKey, a.draftToken); err != nil {
			a.SetNotice("Could not save token: "+err.Error(), true)
			a.Invalidate()
			return
		}
		for key, val := range map[string]string{userSecretKey: a.draftUser, passSecretKey: a.draftPass} {
			if err := a.secrets.Set(key, val); err != nil {
				a.SetNotice("Could not save the login: "+err.Error(), true)
				a.Invalidate()
				return
			}
		}
	}
	a.cfg = cfg
	a.token = a.draftToken
	if a.draftUser != a.portalUser || a.draftPass != a.portalPass {
		a.portalUser, a.portalPass, a.portalInst = a.draftUser, a.draftPass, nil
	}
	a.payslips, a.hasLoaded = nil, false
	a.details = map[string]PayslipData{}
	a.CloseSettings()
	a.ScanLocalFiles()
	a.SetNotice("Settings saved", false)
	if a.IsConfigured() {
		a.Refresh()
	}
	a.Invalidate()
}

// OpenPayslip opens a downloaded payslip with the system PDF viewer.
func (a *App) OpenPayslip(p Payslip) {
	if a.onOpenPayslip != nil {
		a.onOpenPayslip(p)
		return
	}
	path := a.LocalPath(p.Key)
	if path == "" || a.platform == nil {
		return
	}
	if err := a.platform.OpenPath(path); err != nil {
		a.SetNotice(err.Error(), true)
	}
}

// RevealPayslip selects a downloaded payslip in the file manager.
func (a *App) RevealPayslip(p Payslip) {
	if a.onRevealPayslip != nil {
		a.onRevealPayslip(p)
		return
	}
	path := a.LocalPath(p.Key)
	if path == "" || a.platform == nil {
		return
	}
	if err := a.platform.ShowInFolder(path); err != nil {
		a.SetNotice(err.Error(), true)
	}
}

// RevealFolder selects the download folder in the file manager, creating it
// first when missing.
func (a *App) RevealFolder() {
	if a.onRevealFolder != nil {
		a.onRevealFolder()
		return
	}
	if a.cfg.DownloadDir == "" {
		return
	}
	if err := os.MkdirAll(a.cfg.DownloadDir, 0o755); err != nil {
		a.SetNotice(err.Error(), true)
		return
	}
	if a.platform != nil {
		if err := a.platform.ShowInFolder(a.cfg.DownloadDir); err != nil {
			a.SetNotice(err.Error(), true)
		}
	}
}

// SetLaunchAtLogin mirrors the switch into the OS login items.
func (a *App) SetLaunchAtLogin(enabled bool) {
	a.launchAtLogin = enabled
	if a.platform != nil {
		_ = a.platform.SetOpenAtLogin(enabled)
	}
}

// ClosePanel closes the popover, if it is open.
func (a *App) ClosePanel() {
	a.mu.Lock()
	p := a.panel
	a.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

// Quit exits the app.
func (a *App) Quit() {
	if a.platform != nil {
		a.platform.Quit()
	}
}

// showPanel opens the popover below the tray icon, after making the local
// state current. It does nothing when the popover is already open, or when
// there is no tray to anchor it to (headless tests).
func (a *App) showPanel() {
	a.ScanLocalFiles()
	a.loadDetails()
	if !a.hasLoaded && a.IsConfigured() {
		a.Refresh()
	}
	if a.quitting || a.tray == nil {
		return
	}
	a.mu.Lock()
	if a.panel != nil {
		a.mu.Unlock()
		return
	}
	a.panel = mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      a.tray,
		Placement: mygo.PlacementBottom,
		Gap:       6,
		Width:     368,
		Height:    488,
		Focusable: true,
		Shadow:    true,
		Popover:   true,
		Content:   ui.View(a.view),
	})
	a.mu.Unlock()
	a.panel.OnClosed(func() {
		a.mu.Lock()
		a.panel = nil
		a.mu.Unlock()
	})
}

// togglePanel opens the popover, or closes it when it is already open.
func (a *App) togglePanel() {
	a.mu.Lock()
	p := a.panel
	a.mu.Unlock()
	if p != nil {
		p.Close()
		return
	}
	a.showPanel()
}

// QuickLook previews a saved payslip in the system's Quick Look panel.
func (a *App) QuickLook(p Payslip) {
	if a.onQuickLook != nil {
		a.onQuickLook(p)
		return
	}
	path := a.LocalPath(p.Key)
	if path == "" || a.platform == nil {
		return
	}
	if err := a.platform.Preview(path); err != nil {
		a.SetNotice(err.Error(), true)
	}
}

// Refetch has the worker scrape one month from Zucchetti again, which
// replaces the payslip in R2, then rewrites the local file from it.
func (a *App) Refetch(p Payslip) {
	if a.onRefetch != nil {
		a.onRefetch(p)
		return
	}
	if !a.IsConfigured() || a.quitting || a.runningFetch || len(a.refetching) > 0 || p.Year == 0 {
		return
	}
	if a.usesPortal() {
		a.portalSave([]Payslip{p}, a.refetching, false)
		return
	}
	a.refetching[p.Key] = true
	a.Invalidate()
	client := a.clientFor(a.cfg.WorkerURL, a.token)
	key, year, month, dir := p.Key, p.Year, p.Month, a.cfg.DownloadDir
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		fail := func(msg string) {
			a.dispatch(func() {
				delete(a.refetching, key)
				a.SetNotice(msg, true)
				a.Invalidate()
			})
		}
		id, err := client.TriggerRefetch(ctx, year, month)
		if err != nil {
			fail(err.Error())
			return
		}
		st, err := client.WaitForRun(ctx, id, 3*time.Second, nil)
		if err != nil {
			fail("Re-fetch timed out")
			return
		}
		if st.Status != "complete" {
			fail("Re-fetch " + st.Status)
			return
		}
		if _, err := client.DownloadPayslip(ctx, key, dir); err != nil {
			fail(err.Error())
			return
		}
		a.dispatch(func() {
			delete(a.refetching, key)
			a.downloaded[key] = true
			delete(a.details, key)
			a.SetNotice("Re-fetched "+p.Title(), false)
			a.notify("refetch", "Zucchetti", "Re-fetched "+p.Title())
			a.Refresh()
			a.Invalidate()
		})
	}()
}

// loadDetails reads the saved PDFs that have not been read yet, in the
// background. Only parsed numbers are kept.
func (a *App) loadDetails() {
	if a.detailsBusy || a.quitting {
		return
	}
	var keys []string
	for _, p := range a.payslips {
		if _, ok := a.details[p.Key]; !ok && a.downloaded[p.Key] {
			keys = append(keys, p.Key)
		}
	}
	if len(keys) == 0 {
		return
	}
	a.detailsBusy = true
	paths := make(map[string]string, len(keys))
	for _, k := range keys {
		paths[k] = a.LocalPath(k)
	}
	go func() {
		got := make(map[string]PayslipData, len(paths))
		for k, path := range paths {
			d, _ := ExtractPayslip(path) // unreadable: an empty result, not retried
			got[k] = d
		}
		a.dispatch(func() {
			a.detailsBusy = false
			for k, d := range got {
				a.details[k] = d
			}
			a.Invalidate()
		})
	}()
}

// LatestBalances returns the leave balances of the newest payslip that has
// them, with the payslip they come from.
func (a *App) LatestBalances() (Payslip, PayslipData, bool) {
	for _, p := range a.payslips {
		if d, ok := a.details[p.Key]; ok && (d.Ferie != nil || d.ExFs != nil || d.PAR != nil) {
			return p, d, true
		}
	}
	return Payslip{}, PayslipData{}, false
}

// lister returns how payslips are listed for the source in use, and how long
// to wait for it.
func (a *App) lister() (func(context.Context) ([]Payslip, error), time.Duration) {
	if a.usesPortal() {
		return a.portal().List, 3 * time.Minute
	}
	return a.clientFor(a.cfg.WorkerURL, a.token).ListPayslips, 20 * time.Second
}

// portalSave saves payslips from the portal in one session, marking each in
// busy while it waits. all adds a summary notice.
func (a *App) portalSave(items []Payslip, busy map[string]bool, all bool) {
	for _, p := range items {
		busy[p.Key] = true
	}
	a.Invalidate()
	portal, dir := a.portal(), a.cfg.DownloadDir
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(items)+1)*3*time.Minute)
		defer cancel()
		saved, failed := 0, 0
		var firstErr error
		err := portal.Fetch(ctx, items, dir, func(p Payslip, err error) {
			a.dispatch(func() {
				delete(busy, p.Key)
				if err != nil {
					failed++
					if firstErr == nil {
						firstErr = err
					}
				} else {
					saved++
					a.downloaded[p.Key] = true
					delete(a.details, p.Key)
				}
				a.Invalidate()
			})
		})
		a.dispatch(func() {
			if err != nil { // no session: nothing was tried
				for _, p := range items {
					delete(busy, p.Key)
				}
				a.SetNotice(err.Error(), true)
			} else if all || failed > 0 {
				msg := fmt.Sprintf("Saved %d of %d payslips", saved, len(items))
				if firstErr != nil {
					msg += " · " + firstErr.Error()
				}
				a.SetNotice(msg, failed > 0)
			} else if len(items) == 1 {
				a.SetNotice("Saved "+items[0].Title(), false)
			}
			a.loadDetails()
			a.Invalidate()
		})
	}()
}

// portalFetchLatest lists the portal and saves its newest payslip when it is
// not saved yet.
func (a *App) portalFetchLatest() {
	a.runningFetch = true
	a.fetchStatus = "running"
	a.errMsg = ""
	a.Invalidate()
	portal := a.portal()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		items, err := portal.List(ctx)
		a.dispatch(func() {
			a.runningFetch = false
			a.fetchStatus = ""
			a.hasLoaded = true
			if err != nil {
				a.errMsg = err.Error()
				a.Invalidate()
				return
			}
			a.payslips = items
			a.ScanLocalFiles()
			if len(items) > 0 && !a.downloaded[items[0].Key] {
				a.portalSave(items[:1], a.downloading, false)
			} else {
				a.SetNotice("Up to date", false)
			}
			a.loadDetails()
			a.Invalidate()
		})
	}()
}
