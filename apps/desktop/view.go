package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// view renders the dark, pro-tool native macOS menu bar popover UI.
func (a *App) view(c *ui.Context) {
	theme := ui.DarkTheme()
	theme.Background = ui.Hex("#121212")
	theme.Surface = ui.Hex("#1a1a1a")
	theme.SurfaceHover = ui.Hex("#242424")
	theme.SurfacePressed = ui.Hex("#2c2c2c")
	theme.Border = ui.Hex("#2c2c2c")
	theme.Text = ui.Hex("#f2f2f2")
	theme.TextMuted = ui.Hex("#7a7a7a")
	theme.Accent = ui.Hex("#6ea8fe")
	theme.Success = ui.Hex("#4fd6a0")
	theme.Danger = ui.Hex("#ff6b81")
	theme.Warning = ui.Hex("#e8830c")
	theme.Radius = 8
	c.SetTheme(theme)

	// Keyboard shortcuts.
	c.OnShortcut(ui.Cmd, ui.KeyW, a.ClosePanel)
	c.OnShortcut(ui.Cmd, ui.KeyR, a.Refresh)
	c.OnShortcut(ui.Cmd, ui.KeyComma, a.toggleSettings)
	c.OnShortcut(ui.Cmd, ui.KeyQ, a.Quit)
	c.OnShortcut(0, ui.KeyEscape, func() {
		if a.settingsOpen {
			a.CloseSettings()
		} else {
			a.ClosePanel()
		}
	})

	ui.Column(c).
		Width(368).Height(488).
		Padding(12).Gap(10).
		Background(ui.Hex("#121212")).
		Children(func() {
			a.header(c)
			if a.settingsOpen {
				a.settingsView(c)
			} else {
				a.mainView(c)
			}
		})
}

func (a *App) toggleSettings() {
	if a.settingsOpen {
		a.CloseSettings()
	} else {
		a.OpenSettings()
	}
}

// header renders the Zucchetti title, status pill, and chrome icon buttons.
func (a *App) header(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, "Zucchetti").FontSize(15).FontWeight(600).TextColor(ui.Hex("#f2f2f2"))
		a.statusPill(c)
		ui.Spacer(c)
		a.iconButton(c, "arrow.clockwise", svgRefresh, 14, "Refresh", a.Refresh)
		a.iconButton(c, "gearshape", svgGear, 14, "Settings", a.toggleSettings)
	})
}

func (a *App) statusPill(c *ui.Context) {
	var text, kind string
	switch {
	case a.runningFetch:
		text, kind = "Syncing", "info"
	case a.errMsg != "":
		text, kind = "Error", "error"
	case !a.IsConfigured():
		text, kind = "Setup", "error"
	case a.loading || !a.hasLoaded:
		text, kind = "Fetching", "info"
	default:
		text, kind = "Synced", "success"
	}

	var bg, border, col ui.Color
	switch kind {
	case "success":
		bg, col = ui.Hex("#123a2a"), ui.Hex("#4fd6a0")
	case "info":
		bg, col = ui.Hex("#17325c"), ui.Hex("#6ea8fe")
	case "error":
		bg, border, col = ui.Hex("#3a1a24"), ui.Hex("#7a2e44"), ui.Hex("#ff6b81")
	}

	pill := ui.Box(c).Padding(4, 10).Radius(999).Background(bg)
	if kind == "error" {
		pill = pill.Border(1, border)
	}
	pill.Children(func() {
		ui.Text(c, text).FontSize(11).FontWeight(600).TextColor(col)
	})
}

func (a *App) iconButton(c *ui.Context, sfName string, svg *ui.SVG, size float32, label string, action func()) {
	btn := ui.ButtonBase(c).
		Size(28, 28).Radius(999).
		Border(1, ui.Hex("#2e2e2e")).
		Label(label)
	if btn.Clicked() {
		action()
	}
	btn.Children(func() {
		sym(c, sfName, size, ui.Hex("#e8e8e8"), svg)
	})
}

func (a *App) ghostButton(c *ui.Context, label string, action func()) {
	btn := ui.ButtonBase(c).
		Padding(6, 12).Radius(999).
		Border(1, ui.Hex("#2e2e2e")).
		Label(label)
	if btn.Clicked() {
		action()
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(ui.Hex("#e8e8e8"))
	})
}

func (a *App) primaryButton(c *ui.Context, label string, action func()) {
	btn := ui.ButtonBase(c).
		Padding(6, 12).Radius(999).
		Background(ui.Hex("#f5f5f5")).
		Label(label)
	if btn.Clicked() {
		action()
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(ui.Hex("#111111"))
	})
}

func (a *App) card(c *ui.Context) ui.Element {
	return ui.Box(c).
		Background(ui.Hex("#1a1a1a")).
		Border(1, ui.Hex("#2c2c2c")).
		Radius(14)
}

// settingsView renders the inline connection and preferences panels.
func (a *App) settingsView(c *ui.Context) {
	ui.Column(c).Gap(10).Grow(1).MinHeight(0).Children(func() {
		// Connection card.
		a.card(c).Padding(12).Children(func() {
			ui.Column(c).Gap(12).Children(func() {
				a.segmented(c, "source", &a.draftSource, "Zucchetti portal", "Worker")
				if a.draftSource == 0 {
					a.field(c, "Username", "Your Zucchetti username", &a.draftUser, false)
					a.field(c, "Password", "Your Zucchetti password", &a.draftPass, true)
				} else {
					a.field(c, "Worker URL", "https://worker.example.com or http://localhost:8787", &a.draftURL, false)
					a.field(c, "Access token", "Paste token", &a.draftToken, true)
				}

				ui.Column(c).Gap(6).Children(func() {
					ui.Text(c, "Download folder").FontSize(11).TextColor(ui.Hex("#7a7a7a"))
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, shortenHome(a.draftDir)).
							Grow(1).MinWidth(0).
							FontSize(12).
							TextColor(ui.Hex("#a8a8a8")).
							SingleLine()
						a.ghostButton(c, "Choose…", func() { a.ChooseFolder(true) })
					})
				})

				if a.testingConn {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Spinner(c).Size(14, 14).TextColor(ui.Hex("#6ea8fe"))
						ui.Text(c, "Signing in…").FontSize(11).TextColor(ui.Hex("#6ea8fe"))
					})
				} else if a.testConnResult != "" {
					col := ui.Hex("#6ea8fe")
					if a.testConnIsError {
						col = ui.Hex("#ff6b81")
					}
					ui.Text(c, a.testConnResult).FontSize(11).TextColor(col).LineHeight(1.4)
				}
			})
		})

		// Preferences card.
		a.card(c).Padding(12).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Launch at login").FontSize(13).TextColor(ui.Hex("#f2f2f2")).Grow(1)
				ui.Switch(c, &a.launchAtLogin).OnChange(func() {
					a.SetLaunchAtLogin(a.launchAtLogin)
				})
			})
		})

		ui.Spacer(c)

		// Footer actions.
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			a.ghostButton(c, "Quit", a.Quit)
			ui.Spacer(c)
			a.ghostButton(c, "Test", a.TestConnection)
			a.primaryButton(c, "Save", a.SaveSettings)
		})
	})
}

func eyeSymbol(show bool) string {
	if show {
		return "eye.slash"
	}
	return "eye"
}

// mainView renders the tab switcher and active tab content.
func (a *App) mainView(c *ui.Context) {
	ui.Column(c).Gap(10).Grow(1).MinHeight(0).Children(func() {
		a.segmented(c, "tabs", &a.tab, "Payslips", "PTO")

		switch a.tab {
		case TabPTO:
			a.tabPTO(c)
		default:
			a.tabPayslips(c)
		}
	})
}

// tabPayslips renders payslip stats, status, list, and footer.
func (a *App) tabPayslips(c *ui.Context) {
	ui.Column(c).Gap(10).Grow(1).MinHeight(0).Children(func() {
		// Stats + folder card.
		a.card(c).Children(func() {
			ui.Column(c).Children(func() {
				// Stats.
				latest := "—"
				if len(a.payslips) > 0 {
					latest = a.payslips[0].MonthName()[:3] + fmt.Sprintf(" %d", a.payslips[0].Year)
				}
				stats := []struct {
					label string
					value string
				}{
					{a.listedLabel(), fmt.Sprintf("%d", len(a.payslips))},
					{"Saved", fmt.Sprintf("%d", a.DownloadedCount())},
					{"Latest", latest},
				}
				ui.Row(c).Height(64).Dividers(1, ui.Hex("#262626")).Children(func() {
					for _, st := range stats {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, st.value).
								FontSize(17).
								FontWeight(500).
								FontFeatures("tnum").
								TextColor(ui.Hex("#f2f2f2")).
								Center()
							ui.Text(c, st.label).
								FontSize(10).
								TextColor(ui.Hex("#7a7a7a")).
								Center()
						})
					}
				})

				ui.Divider(c).Height(1).Background(ui.Hex("#262626"))

				// Folder strip.
				strip := ui.Row(c).Height(40).Padding(0, 12).AlignItems(ui.Center).Gap(8)
				if strip.Hovered() {
					strip.Background(ui.RGBA(255, 255, 255, 0.045))
				}
				strip.Children(func() {
					reveal := ui.ButtonBase(c).Grow(1).MinWidth(0).Label("Reveal folder").Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).MinWidth(0).Children(func() {
							sym(c, "folder", 14, ui.Hex("#a8a8a8"), svgFolder)
							ui.Text(c, shortenHome(a.cfg.DownloadDir)).
								FontSize(12).
								TextColor(ui.Hex("#a8a8a8")).
								SingleLine()
						})
					})
					if reveal.Clicked() {
						a.RevealFolder()
					}

					change := ui.ButtonBase(c).Label("Change folder").Children(func() {
						ui.Text(c, "Change").FontSize(12).TextColor(ui.Hex("#e8e8e8"))
					})
					if change.Clicked() {
						a.ChooseFolder(false)
					}
				})
			})
		})

		// Status banner.
		a.payslipBanner(c)

		// Payslips list card.
		a.card(c).Grow(1).MinHeight(0).Clip().Children(func() {
			a.payslipsList(c)
		})

		// Footer.
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			a.ghostButton(c, "Fetch latest", a.TriggerFetch)
			ui.Spacer(c)
			if a.UndownloadedCount() > 0 {
				a.primaryButton(c, fmt.Sprintf("Download all (%d)", a.UndownloadedCount()), a.DownloadAll)
			} else {
				ui.Text(c, fmt.Sprintf("%d saved", a.DownloadedCount())).
					FontSize(12).
					TextColor(ui.Hex("#7a7a7a")).
					FontFeatures("tnum")
				a.ghostButton(c, "Open folder", a.RevealFolder)
			}
		})
	})
}

func (a *App) payslipBanner(c *ui.Context) {
	switch {
	case a.errMsg != "":
		banner := ui.Box(c).
			Padding(10, 12).
			Radius(10).
			Background(ui.Hex("#3a1a24")).
			Border(1, ui.Hex("#7a2e44"))
		banner.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, a.errMsg).FontSize(12).TextColor(ui.Hex("#ff6b81")).Grow(1).LineHeight(1.3)
				a.ghostButton(c, "Retry", a.Refresh)
			})
		})
	case a.runningFetch:
		banner := ui.Box(c).
			Padding(10, 12).
			Radius(10).
			Background(ui.Hex("#17325c"))
		banner.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Spinner(c).TextColor(ui.Hex("#6ea8fe"))
				ui.Text(c, "Fetching payslips…").FontSize(12).TextColor(ui.Hex("#6ea8fe"))
				ui.Spacer(c)
			})
		})
	case a.noticeMsg != "" && a.noticeUntil.After(c.Now()):
		banner := ui.Box(c).
			Padding(10, 12).
			Radius(10).
			Background(ui.Hex("#17325c"))
		banner.Children(func() {
			ui.Text(c, a.noticeMsg).FontSize(12).TextColor(ui.Hex("#6ea8fe"))
		})
	}
}

func (a *App) payslipsList(c *ui.Context) {
	switch {
	case !a.IsConfigured():
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, "Connect to Zucchetti").
				FontSize(13).
				TextColor(ui.Hex("#a8a8a8")).
				Center()
			a.primaryButton(c, "Configure connection", a.OpenSettings)
		})
	case a.loading && !a.hasLoaded:
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Spinner(c).TextColor(ui.Hex("#6ea8fe"))
			ui.Text(c, "Loading payslips…").
				FontSize(13).
				TextColor(ui.Hex("#a8a8a8"))
		})
	case len(a.payslips) == 0:
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, "No payslips archived yet").
				FontSize(13).
				TextColor(ui.Hex("#a8a8a8")).
				Center()
			a.primaryButton(c, "Fetch current month", a.TriggerFetch)
		})
	default:
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).Children(func() {
				for _, g := range payslipYears(a.payslips) {
					a.yearHeader(c, g)
					for i, p := range g.items {
						a.payslipRow(c, p, i == len(g.items)-1)
					}
				}
			})
		})
	}
}

func (a *App) payslipRow(c *ui.Context, p Payslip, last bool) {
	row := ui.Row(c).Height(48).Padding(0, 12).AlignItems(ui.Center).Gap(8)
	if row.Hovered() {
		row.Background(ui.RGBA(255, 255, 255, 0.045))
	}
	row.Children(func() {
		actionLabel := "Quick Look " + p.Title()
		if !a.downloaded[p.Key] {
			actionLabel = "Download " + p.Title()
		}
		left := ui.ButtonBase(c).Grow(1).MinWidth(0).Justify(ui.Start).Label(actionLabel).Children(func() {
			ui.Column(c).Justify(ui.Center).AlignItems(ui.Start).Children(func() {
				ui.Text(c, p.MonthName()).FontSize(13).FontWeight(500).TextColor(ui.Hex("#f2f2f2")).NoWrap().TextAlign(ui.Start)
				sub := ui.Hex("#7a7a7a")
				if a.periodMismatch(p) {
					sub = ui.Hex("#ff6b81")
				}
				ui.Text(c, a.rowSubtitle(p)).
					FontSize(11).
					TextColor(sub).
					FontFeatures("tnum").
					SingleLine().TextAlign(ui.Start)
			})
		})
		if left.Clicked() {
			if a.downloaded[p.Key] {
				a.QuickLook(p)
			} else {
				a.Download(p)
			}
		}

		switch {
		case a.downloading[p.Key]:
			ui.Spinner(c).Size(18, 18).TextColor(ui.Hex("#6ea8fe")).Label("Downloading " + p.Title())
		case a.refetching[p.Key]:
			ui.Spinner(c).Size(18, 18).TextColor(ui.Hex("#6ea8fe")).Label("Re-fetching " + p.Title())
		default:
			if a.downloaded[p.Key] {
				a.iconButton(c, "eye", svgEye, 13, "Quick Look "+p.Title(), func() { a.QuickLook(p) })
			} else {
				a.iconButton(c, "arrow.down.circle", svgDownload, 13, "Download "+p.Title(), func() { a.Download(p) })
			}
			a.iconButton(c, "arrow.triangle.2.circlepath", svgRefresh, 13, "Re-fetch "+p.Title(), func() { a.Refetch(p) })
			if a.downloaded[p.Key] {
				a.iconButton(c, "arrow.up.forward.square", svgExternal, 13, "Reveal "+p.Title(), func() { a.RevealPayslip(p) })
			}
		}
	})

	if !last {
		ui.Divider(c).Height(1).Background(ui.Hex("#242424")).Margin(0, 12)
	}
}

// tabPTO renders the calendar, overview, and footer for PTO planning.
func (a *App) tabPTO(c *ui.Context) {
	ui.Column(c).Gap(10).Grow(1).MinHeight(0).Children(func() {
		// The cards scroll; the footer stays in view.
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Gap(10).Children(func() {
				a.card(c).Padding(12).Children(func() {
					// The grid's cells have a fixed size: center it, and the
					// header above it takes the grid's width.
					ui.Column(c).AlignItems(ui.Center).Children(func() {
						ui.Calendar(c.Key("pto-calendar"), &a.ptoDate).Label("PTO calendar")
					})
				})
				a.card(c).Padding(12).Children(func() {
					ui.Column(c).Gap(10).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Children(func() {
							ui.Text(c, a.ptoDate.Format("Monday, Jan 2, 2006")).
								FontSize(14).FontWeight(500).TextColor(ui.Hex("#f2f2f2")).
								FontFeatures("tnum").Grow(1)
							ui.Box(c).Padding(4, 10).Radius(999).Background(ui.Hex("#17325c")).Children(func() {
								ui.Text(c, "Requests soon").FontSize(11).FontWeight(600).TextColor(ui.Hex("#6ea8fe"))
							})
						})
						a.balanceCard(c)
					})
				})
			})
		})

		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Selected: "+a.ptoDate.Format("Jan 2")).
				FontSize(12).TextColor(ui.Hex("#a8a8a8")).FontFeatures("tnum").Grow(1)
			ui.ButtonBase(c).Padding(6, 12).Radius(999).Border(1, ui.Hex("#2e2e2e")).
				Disabled(true).Label("Request PTO").Children(func() {
				ui.Text(c, "Request PTO").FontSize(12).TextColor(ui.Hex("#7a7a7a"))
			})
		})
	})
}

// rowSubtitle is the second line of a payslip row: what the PDF says when it
// has been read, else its size and upload date.
func (a *App) rowSubtitle(p Payslip) string {
	if a.periodMismatch(p) {
		return "PDF is " + a.details[p.Key].Period + ", not " + p.MonthName()
	}
	if d, ok := a.details[p.Key]; ok && d.HasNet {
		s := "€" + d.Net() + " net"
		if d.Period != "" {
			s += " · " + d.Period
		}
		return s
	}
	switch {
	case p.Size > 0 && !p.Uploaded.IsZero():
		return fmt.Sprintf("%s · %s", FormatSize(p.Size), p.Uploaded.Format("Jan 2"))
	case !p.Uploaded.IsZero():
		return "Published " + p.Uploaded.Format("Jan 2")
	default:
		return "Not saved yet"
	}
}

type balanceRow struct {
	name  string
	color string
	b     *Balance
}

// balanceRows lists the leave accounts the payslip gave, in the PTO legend's
// colors.
func balanceRows(d PayslipData) []balanceRow {
	var out []balanceRow
	for _, r := range []balanceRow{
		{"Vacation", "#e8830c", d.Ferie},
		{"PAR leave", "#6ea8fe", d.PAR},
		{"Ex-holidays", "#2ec4b0", d.ExFs},
	} {
		if r.b != nil {
			out = append(out, r)
		}
	}
	return out
}

// days formats hours as 8-hour days: "7.3 d".
func days(h float64) string {
	return fmt.Sprintf("%.1f d", h/8)
}

// balanceCard shows the leave left, read from the newest payslip.
func (a *App) balanceCard(c *ui.Context) {
	p, d, ok := a.LatestBalances()
	if !ok {
		ui.Text(c, "Download a payslip to see your leave balance.").
			FontSize(12).TextColor(ui.Hex("#7a7a7a")).LineHeight(1.4)
		return
	}
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Leave left").FontSize(13).FontWeight(500).TextColor(ui.Hex("#f2f2f2")).Grow(1)
		src := p.Title()
		if d.Period != "" {
			src = d.Period
		}
		ui.Text(c, "from "+src).FontSize(11).TextColor(ui.Hex("#7a7a7a"))
	})
	for _, r := range balanceRows(d) {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Box(c).Size(8, 8).Radius(2).Background(ui.Hex(r.color))
			ui.Text(c, r.name).FontSize(12).TextColor(ui.Hex("#a8a8a8")).Grow(1)
			ui.Text(c, fmt.Sprintf("%.1f h · %s", r.b.Remaining, days(r.b.Remaining))).
				FontSize(12).FontFeatures("tnum").TextColor(ui.Hex("#f2f2f2"))
		})
	}
}

// periodMismatch reports a saved PDF whose printed period is not the month of
// its file name: the portal sometimes serves another month's document.
func (a *App) periodMismatch(p Payslip) bool {
	d, ok := a.details[p.Key]
	if !ok || p.Year == 0 {
		return false
	}
	y, m, ok := d.PeriodYM()
	return ok && (y != p.Year || m != p.Month)
}

type yearGroup struct {
	year  int
	items []Payslip
}

// payslipYears splits payslips, newest first, into one group per year, in
// the order they come. A payslip without a year gets a group of year 0.
func payslipYears(items []Payslip) []yearGroup {
	var out []yearGroup
	for _, p := range items {
		if n := len(out); n == 0 || out[n-1].year != p.Year {
			out = append(out, yearGroup{year: p.Year})
		}
		out[len(out)-1].items = append(out[len(out)-1].items, p)
	}
	return out
}

// yearHeader is the line above a year's payslips: the year, and how many of
// them are saved.
func (a *App) yearHeader(c *ui.Context, g yearGroup) {
	saved := 0
	for _, p := range g.items {
		if a.downloaded[p.Key] {
			saved++
		}
	}
	title := "Other"
	if g.year > 0 {
		title = fmt.Sprintf("%d", g.year)
	}
	ui.Row(c).AlignItems(ui.Center).Padding(10, 12, 6, 12).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(ui.Hex("#a8a8a8")).Grow(1)
		ui.Text(c, fmt.Sprintf("%d of %d saved", saved, len(g.items))).
			FontSize(11).FontFeatures("tnum").TextColor(ui.Hex("#7a7a7a"))
	})
	ui.Divider(c).Height(1).Background(ui.Hex("#242424"))
}

// segmented is the dark segmented control: a track with the chosen segment
// raised.
func (a *App) segmented(c *ui.Context, key string, sel *int, labels ...string) {
	seg := ui.SegmentedBase(c.Key(key), sel, len(labels))
	seg.Track.
		FillWidth().
		Padding(3).Gap(3).
		Background(ui.Hex("#141414")).
		Border(1, ui.Hex("#262626")).
		Radius(10).
		Children(func() {
			for i, label := range labels {
				s := seg.Segment(i).
					Radius(7).
					Padding(6, 0).
					FillHeight()
				if i == *sel {
					s.Background(ui.Hex("#2a2a2a")).Border(1, ui.Hex("#383838"))
				}
				s.Children(func() {
					ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(ui.Hex("#f2f2f2")).Center()
				})
			}
		})
	seg.Track.FillEqually()
}

// field is a labelled text input in the settings card; secret ones hide
// their text unless a.showToken is on, with an eye button.
func (a *App) field(c *ui.Context, label, placeholder string, value *string, secret bool) {
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(ui.Hex("#7a7a7a"))
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			in := ui.TextInputBase(c.Key(label), value).
				Grow(1).
				Background(ui.Hex("#141414")).
				Border(1, ui.Hex("#262626")).
				Radius(7).
				Padding(8, 10).
				TextColor(ui.Hex("#f2f2f2")).
				Placeholder(placeholder).
				Label(label + " input").
				OnChange(func() { a.settingsDirty = true })
			if secret && !a.showToken {
				in.Password()
			}
			if secret {
				a.iconButton(c, eyeSymbol(a.showToken), svgEye, 14, "Toggle "+strings.ToLower(label)+" visibility", func() {
					a.showToken = !a.showToken
				})
			}
		})
	})
}

func (a *App) listedLabel() string {
	if a.usesPortal() {
		return "On portal"
	}
	return "Archived"
}
