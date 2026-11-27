# desktop

A macOS menu bar app for your Zucchetti payslips. Click the icon to list your payslips, download them into a folder you choose, and read the leave you have left. It reads them straight from the Zucchetti portal, or from a [worker](../worker) that archives them for you. It is written in Go with the native UI of [MyGo](https://github.com/egoist/mygo): no webview, no JavaScript, one small binary.

## Two sources

- **Zucchetti portal** (default) — the app signs in to the HR portal itself, in a window you don't see, and lists every payslip the portal has (back to the first one it still keeps). It needs no server. Your username and password are stored in the macOS Keychain. A session takes about 15 seconds to sign in; after that each PDF takes a second or two. A payslip you open from the list, or *Fetch latest*, uses one session.
- **Worker** — the [worker](../worker) downloads payslips on a schedule, keeps them in R2 and emails them. The app lists what it has archived. Use this if you want payslips fetched while your Mac is off.

Pick the source in Settings. Everything else works the same with both.

## What it does

- **List and download** — payslips newest first, grouped by year. Files are saved as `YYYY-MM.pdf` (e.g. `2026-11.pdf`); older `busta-YYYY-MM.pdf` files in the folder are renamed on first run. A row downloads the PDF into your folder, or opens Quick Look if it is already saved. *Download all* fetches the missing ones.
- **Re-fetch** — the ↻ button on a row gets that month from the source again and rewrites the local file. With the worker, it makes the worker scrape Zucchetti again, without an email. Use it when a file is wrong.
- **Fetch latest** — with the portal, lists it and saves the newest payslip if you don't have it. With the worker, starts a worker run for the current month and follows it.
- **Background check** — while the app is running, checks on launch, every six hours, when the Mac wakes, and when the network returns. The payslip list is cached in `payslips-cache.json` so it appears immediately at startup; newly listed payslips are downloaded automatically, with a notification.
- **Read the PDF** — saved payslips are parsed on your machine. A row shows the net pay and the period printed in the PDF, and the PTO tab shows the leave left (vacation, PAR and ex-holiday hours) from the newest payslip. The **Trends** tab charts monthly take-home pay and remaining leave hours from locally saved PDFs; missing or mismatched periods are shown as gaps. A balance is only shown if its numbers add up. If the period in a PDF is not the month of its file name, the row turns red.
- **PTO** — a calendar tab, reserved for tracking and requesting leave. Requests are not built yet.

Payslip PDFs and the list cache stay on your Mac. The app talks either to the Zucchetti portal or to your worker; it does not upload PDFs or extracted text.

## Setup

Open the app, click the gear icon (or press Cmd+,), choose the source and fill it in.

**Zucchetti portal** — your Zucchetti **username** and **password**, and the **download folder** (default `~/Documents/Payslips`).

**Worker** — deploy the [worker](../worker) first, then set:

- **Worker URL** — your deployed Worker, or `http://localhost:8787` for a local one (`http://` is the default for `localhost`).
- **Access token** — the worker's `RUN_TOKEN`. It is stored in the macOS Keychain, never in a file.
- **Download folder** — defaults to `~/Documents/Payslips`.

*Test* signs in (or reaches the worker) before you *Save*. The other settings are in `~/Library/Application Support/Zucchetti/config.json`.

## Commands

You need Go 1.27 or later. From the repo root, `mise //apps/desktop:<task>`; from this directory, `mise :<task>`.

| Task | |
|---|---|
| `dev` | run with live reload (`go tool mygo dev`) |
| `build` | package `build/darwin-arm64/Zucchetti.app` and a `.dmg` |
| `open` | build, then open the app |
| `test` | `go vet` and `go test`; the views are tested without a window |
| `doctor` | check the machine has what MyGo needs |
| `portal-check` | sign in to the portal with `ZUCCHETTI_USER` and `ZUCCHETTI_PASS`, list the payslips and save the newest two to a temp folder: a live test of the portal source |

Shortcuts: Cmd+R refreshes, Cmd+, opens settings, Esc closes, Cmd+Q quits.

## Code

| | |
|---|---|
| `main.go` | the tray icon, the popover, and the macOS integration |
| `view.go` | the popover UI, a dark theme with hairline borders |
| `state.go`, `controller.go` | app state and the background work: refresh, download, re-fetch, polling |
| `client.go` | the worker's HTTP API |
| `portal.go` | the portal source: a web window that signs in, walks the menus, lists the documents and downloads PDFs |
| `extract.go` | reads period, net pay and leave balances from a payslip PDF, with the arithmetic checks |
| `config.go` | settings and the Keychain token |
| `icons.go` | SF Symbols with SVG fallbacks, and the menu bar icon |

`.impeccable.md` holds the design context behind the UI.

Tests use invented numbers and a PDF built in code, so no payslip is committed. To try the parser on a real file: `PAYSLIP_PDF=/path/to/busta.pdf go test -run Real -v`. To write PNGs of the screens: `SNAP_DIR=/tmp/shots go test -run Snapshots`.

## How the portal source works, and its quirks

The portal has no API, so the app drives it like a person would, in a web window (WebKit) with zero opacity, no frame, no taskbar entry, and no mouse or focus. Two things were learned the hard way:

- The window has to count as *shown*. In a hidden window WebKit does not run animations, and the portal opens its PDF viewer after one.
- The portal opens the row that is **selected**, not the row whose link was clicked, and a plain `click()` doesn't select it. Without the mouse-down and mouse-up first, every payslip but the first opens the first one's document. This is what once put October's PDF under November's name. Each PDF's printed period is also checked against the month, and fetched again once if it differs.

If the portal starts asking for a code at sign-in, the app reports that it could not sign in.

## Limits

- The portal source relies on the portal's page structure (menu and frame ids) as of November 2026; if Zucchetti redesigns it, `portal.go` needs updating.
- Background checks run while the app is running (enable **Launch at login** in Settings to keep it available). If you quit the app or the Mac is off, only the worker can fetch payslips.
- Certificazione Unica documents on the portal are not listed yet.
- macOS only for now. It builds for Linux and Windows through MyGo, but I have only used it on macOS.
- The PDF parser was written against one employer's payslip layout. A different layout shows nothing rather than a wrong figure.
- Unsigned builds: macOS may ask you to allow the app on first launch.
