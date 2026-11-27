package main

import (
	"log"
	"runtime"

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

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
			a.tray = nil
		}
	})
	mygo.App.WhenReady(a.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
