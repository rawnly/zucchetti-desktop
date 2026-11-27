# desktop

A macOS menu bar app for your Zucchetti payslips. Click the icon to list the payslips the [worker](../worker) has archived, download them into a folder you choose, and read the leave you have left. It is written in Go with the native UI of [MyGo](https://github.com/egoist/mygo): no webview, no JavaScript, one small binary.

## What it does

- **List and download** — payslips newest first, grouped by year. Files are saved as `YYYY-MM.pdf` (e.g. `2026-11.pdf`); older `busta-YYYY-MM.pdf` files in the folder are renamed on first run. A row downloads the PDF into your folder, or opens Quick Look if it is already saved. *Download all* fetches the missing ones.
- **Re-fetch** — the ↻ button on a row asks the worker to scrape that month from Zucchetti again, without an email, then rewrites the local file. Use it when a file is wrong.
- **Fetch latest** — starts a worker run for the current month and follows it until it completes.
- **Read the PDF** — saved payslips are parsed on your machine. A row shows the net pay and the period printed in the PDF, and the PTO tab shows the leave left (vacation, PAR and ex-holiday hours) from the newest payslip. A balance is only shown if its numbers add up. If the period in a PDF is not the month of its file name, the row turns red.
- **PTO** — a calendar tab, reserved for tracking and requesting leave. Requests are not built yet.

Everything the app reads stays on your Mac. It only talks to your worker.

## Setup

Deploy the [worker](../worker) first. Then open the app, click the gear icon (or press Cmd+,) and set:

- **Worker URL** — your deployed Worker, or `http://localhost:8787` for a local one (`http://` is the default for `localhost`).
- **Access token** — the worker's `RUN_TOKEN`. It is stored in the macOS Keychain, never in a file.
- **Download folder** — defaults to `~/Documents/Payslips`.

*Test* checks the connection before you *Save*. The other settings are in `~/Library/Application Support/Zucchetti/config.json`.

## Commands

You need Go 1.27 or later. From the repo root, `mise //apps/desktop:<task>`; from this directory, `mise :<task>`.

| Task | |
|---|---|
| `dev` | run with live reload (`go tool mygo dev`) |
| `build` | package `build/darwin-arm64/Zucchetti.app` and a `.dmg` |
| `open` | build, then open the app |
| `test` | `go vet` and `go test`; the views are tested without a window |
| `doctor` | check the machine has what MyGo needs |

Shortcuts: Cmd+R refreshes, Cmd+, opens settings, Esc closes, Cmd+Q quits.

## Code

| | |
|---|---|
| `main.go` | the tray icon, the popover, and the macOS integration |
| `view.go` | the popover UI, a dark theme with hairline borders |
| `state.go`, `controller.go` | app state and the background work: refresh, download, re-fetch, polling |
| `client.go` | the worker's HTTP API |
| `extract.go` | reads period, net pay and leave balances from a payslip PDF, with the arithmetic checks |
| `config.go` | settings and the Keychain token |
| `icons.go` | SF Symbols with SVG fallbacks, and the menu bar icon |

`.impeccable.md` holds the design context behind the UI.

Tests use invented numbers and a PDF built in code, so no payslip is committed. To try the parser on a real file: `PAYSLIP_PDF=/path/to/busta.pdf go test -run Real -v`. To write PNGs of the screens: `SNAP_DIR=/tmp/shots go test -run Snapshots`.

## Limits

- macOS only for now. It builds for Linux and Windows through MyGo, but I have only used it on macOS.
- The PDF parser was written against one employer's payslip layout. A different layout shows nothing rather than a wrong figure.
- Unsigned builds: macOS may ask you to allow the app on first launch.
