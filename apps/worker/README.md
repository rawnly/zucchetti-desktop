# worker

A Cloudflare Worker that downloads your monthly payslip from the Zucchetti HR Progest portal, keeps it in a private R2 bucket, and emails it to you. It also exposes a small authenticated API, which the [desktop app](../desktop) uses.

It is Effect-native and deployed with [Alchemy](https://alchemy.run). It uses Cloudflare Browser Rendering to drive the portal, and Cloudflare's own Email Service to send mail, so there are no third-party services.

## How it runs

- **Schedule** — a Workflow runs on the 27th of each month at 18:00 Europe/Rome. It is triggered at both 16:00 and 17:00 UTC and skips the one that is not 18:00 in Rome, so daylight saving needs no change.
- **Steps** — log in to the portal, open *I miei documenti*, click the row `Libro unico di MM-YYYY` for the current month, and read the PDF through the viewer. It then stores it as `busta-YYYY-MM.pdf` in R2 and emails it as an attachment. Each step retries on its own. If the run fails, you get an error email.
- **Guard** — the portal sometimes serves one month's document for another month's row. The run fails and retries if the PDF is byte-identical to any other month already in R2, and it warns by email if it matches the previous month's.

## HTTP API

Every route needs the header `x-run-token: <RUN_TOKEN>` (compared in constant time). Without it, or for an unknown route, the worker answers 404.

| Route | Does |
|---|---|
| `GET /buste` | `{ items: [{ key, year, month, size, uploaded, etag }] }`, newest first |
| `GET /buste/:key` | the PDF; `key` must be `busta-YYYY-MM.pdf` |
| `POST /run[?date=YYYY-MM-DD][&notify=0]` | start a run for the month of `date` (default: now); `notify=0` skips the email |
| `GET /status/:id` | status of a run started with `POST /run` |

```sh
curl -X POST "$WORKER_URL/run" -H "x-run-token: $RUN_TOKEN"
curl "$WORKER_URL/buste" -H "x-run-token: $RUN_TOKEN"
```

## Setup

You need a Cloudflare account on **Workers Paid** (Browser Rendering requires it) and a domain on a Cloudflare zone for the sending address.

```sh
npm install
cp .env.example .env     # then fill it in
```

| Variable | |
|---|---|
| `ZUCCHETTI_USER`, `ZUCCHETTI_PASS` | portal login |
| `RUN_TOKEN` | secret for the HTTP API; use a long random string |
| `MAIL_FROM` | sender, on a domain in your Cloudflare zone, e.g. `buste-paga@mail.example.com` |
| `MAIL_TO` | recipient; Cloudflare emails it a link to verify |

Authenticate with `CLOUDFLARE_ACCOUNT_ID` and an API token, or log in with `wrangler`. These values become Worker secrets at deploy time.

## Commands

From the repo root, `mise //apps/worker:<task>`; from this directory, `mise :<task>`.

| Task | |
|---|---|
| `deploy` | `alchemy deploy`: the R2 bucket, the sending subdomain and address, the Worker and its cron |
| `dev` | `alchemy dev` |
| `typecheck` | `tsc --noEmit` |
| `test` | unit tests of `src/` |

After the first deploy, verify the recipient address from the email Cloudflare sends.

## Code

- `alchemy.run.ts` — the stack: private R2 bucket `BustePaga`, email sending subdomain and recipient address, the Worker.
- `src/worker.ts` — the HTTP routes above.
- `src/workflow.ts` — the scheduled run: download, store, email, and the guards.
- `src/zucchetti.ts` — the portal flow with puppeteer.
- `src/busta.ts` — key, label and token helpers (unit tested).
- `vendor/` — pinned effect and alchemy sources (`vendors.json`), kept only as reference for reading the code. Packages install from npm.

TypeScript must be **6.0.3**; effect 4.1.4 does not compile on 5.9.

## Limits

- If the portal adds OTP or SSO to the login, the automation stops working.
- The selectors match the portal's layout as of November 2026, and the row name `Libro unico di MM-YYYY Mensili`. If either changes, update `src/zucchetti.ts`.
- The duplicate guard only catches a document already stored for another month. A wrong document with no copy in R2 is not detected by the worker; the desktop app flags it when the period printed in the PDF does not match the month.
