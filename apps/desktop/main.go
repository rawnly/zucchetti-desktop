package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
)

// desktopPlatform implements Platform with MyGo's desktop APIs.
type desktopPlatform struct{}

func (desktopPlatform) OpenPath(path string) error {
	return mygo.Shell.OpenPath(path)
}

func (desktopPlatform) ShowInFolder(path string) error {
	mygo.Shell.ShowItemInFolder(path)
	return nil
}

func (desktopPlatform) ChooseDirectory(title, current string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:       title,
		DefaultPath: current,
		Directory:   true,
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}

func (desktopPlatform) Preview(path string) error {
	return mygo.Shell.PreviewItem(path)
}

func (desktopPlatform) CopyText(text string) {
	mygo.Clipboard.WriteText(text)
}

func (desktopPlatform) Notify(id, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	_ = mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body}).Show()
}

func (desktopPlatform) OpenAtLogin() bool {
	return mygo.App.OpenAtLogin()
}

func (desktopPlatform) SetOpenAtLogin(enabled bool) error {
	return mygo.App.SetOpenAtLogin(enabled)
}

func (desktopPlatform) Quit() {
	mygo.App.Quit()
}

// ready creates the tray icon, wires its clicks, and loads the saved state.
// Runs on the main thread once the application is ready.
func (a *App) ready() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:           trayIconPNG(),
		IconIsTemplate: true,
		ToolTip:        "Zucchetti — Payslips",
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	a.tray = tray

	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Open Zucchetti", Click: func(*mygo.MenuItem, *mygo.Window) { a.showPanel() }},
		{Label: "Fetch current month", Click: func(*mygo.MenuItem, *mygo.Window) { a.TriggerFetch() }},
		{Label: "Open payslips folder", Click: func(*mygo.MenuItem, *mygo.Window) { a.RevealFolder() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
	if runtime.GOOS == "linux" {
		// AppIndicator shows menus only and reports no clicks.
		a.tray.SetMenu(menu)
	} else {
		a.tray.OnClick(a.togglePanel)
		a.tray.OnRightClick(func() {
			a.ClosePanel()
			// The menu is for this popup only, so left clicks keep toggling
			// the popover.
			a.tray.SetMenu(menu)
			defer a.tray.SetMenu(nil)
			a.tray.PopUpMenu()
		})
	}

	a.LoadInitialState()
	a.startBackgroundRefresh()
}

// startBackgroundRefresh keeps the tray app out of App Nap and checks for
// new payslips every six hours, when the network returns, and after sleep.
func (a *App) startBackgroundRefresh() {
	ctx, cancel := context.WithCancel(context.Background())
	release := mygo.Power.KeepRunning("Checking for new Zucchetti payslips")
	offResume := mygo.Power.OnResume(func() { a.dispatch(a.Refresh) })
	offNetwork := mygo.Network.OnChanged(func(s mygo.NetworkStatus) {
		if s.Online {
			a.dispatch(a.Refresh)
		}
	})
	a.stopBackground = func() {
		cancel()
		offResume()
		offNetwork()
		release()
	}
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.dispatch(a.Refresh)
			}
		}
	}()
}

func mustConfigPath() string {
	path, err := configPath()
	if err != nil {
		log.Fatal("config path: ", err)
	}
	return path
}

func main() {
	mygo.App.SetName("Zucchetti")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// The popover is the app's only window: keep running when it closes.
	mygo.App.OnWindowAllClosed(func() {})

	a := NewApp(mustConfigPath(), mygoSecrets{}, desktopPlatform{})

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		if a.stopBackground != nil {
			a.stopBackground()
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
			a.tray = nil
		}
	})
	if len(os.Args) > 1 && os.Args[1] == "--portal-check" {
		mygo.App.WhenReady(portalCheck)
	} else {
		mygo.App.WhenReady(a.ready)
	}
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// portalCheck signs in to the Zucchetti portal with ZUCCHETTI_USER and
// ZUCCHETTI_PASS, lists the payslips and saves the newest two into a temp
// folder, to see that the portal source works end to end:
//
//	mise //apps/desktop:portal-check
func portalCheck() {
	go func() {
		code := 0
		defer func() { mygo.RunOnMain(func() { os.Exit(code) }) }()
		user, pass := os.Getenv("ZUCCHETTI_USER"), os.Getenv("ZUCCHETTI_PASS")
		if user == "" || pass == "" {
			fmt.Println("set ZUCCHETTI_USER and ZUCCHETTI_PASS")
			code = 2
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		start := time.Now()
		p := newWebPortal(user, pass)
		items, err := p.List(ctx)
		if err != nil {
			fmt.Println("list failed:", err)
			code = 1
			return
		}
		fmt.Printf("[%4.0fs] %d payslips, %s to %s\n", time.Since(start).Seconds(), len(items), items[len(items)-1].Key, items[0].Key)
		for _, it := range []Payslip{items[0], items[len(items)-1]} {
			fmt.Printf("         %s published %s\n", it.Key, it.Uploaded.Format("2006-01-02 15:04"))
		}
		dir, _ := os.MkdirTemp("", "zucchetti-check-")
		defer os.RemoveAll(dir)
		take := items[:min(2, len(items))]
		err = p.Fetch(ctx, take, dir, func(it Payslip, err error) {
			if err != nil {
				fmt.Printf("[%4.0fs] %s: FAILED %v\n", time.Since(start).Seconds(), it.Key, err)
				code = 1
				return
			}
			name, _ := LocalFileName(it.Key)
			path := filepath.Join(dir, name)
			b, _ := os.ReadFile(path)
			d, _ := ExtractPayslip(path)
			fmt.Printf("[%4.0fs] %s: %d bytes md5=%x period=%q net=%v\n", time.Since(start).Seconds(), name, len(b), md5.Sum(b), d.Period, d.HasNet)
		})
		if err != nil {
			fmt.Println("fetch failed:", err)
			code = 1
		}
	}()
}
