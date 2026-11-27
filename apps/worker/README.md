# buste-paga-cf

Scarica automaticamente la busta paga del mese corrente dal portale Zucchetti
(HR Progest) e la salva in un bucket R2 privato. Un Cloudflare Worker
Effect-native gira il **27 di ogni mese** (06:00 UTC) con
Browser Rendering (il browser agent di Cloudflare) e notifica via **email
nativa Cloudflare** (Email Service, binding `send_email`). Zero terze parti.

## Setup

```sh
# 1. vendor: sorgenti pina­ti per SHA (effect + monorepo alchemy)
vendor install

# 2. deps (il preinstall patcha i manifest vendorati, vedi sotto)
npm install

# 3. secrets nel .env (o nell'ambiente del deploy):
cp .env.example .env
```

`.env` (mai committato):

```
ZUCCHETTI_USER=...
ZUCCHETTI_PASS=...
RUN_TOKEN=...                 # per il trigger manuale POST /run
MAIL_FROM=buste-paga@mail.<tuo-dominio>   # dominio su zona Cloudflare
MAIL_TO=<tua-email>            # va verificata cliccando l'email di Cloudflare
```

Serve un account Cloudflare con Workers Paid (Browser Rendering lo richiede):
`CLOUDFLARE_ACCOUNT_ID` + API token, o login con `wrangler`.

## Comandi

```sh
npm test        # test sync-vendor (node:test)
npm run typecheck
npm run deploy  # alchemy deploy (stack: R2 privato + Worker + cron)
```

Trigger manuale (scarica la busta del mese corrente fuori dal cron):

```sh
curl -X POST "$WORKER_URL/run" -H "x-run-token: $RUN_TOKEN"
```

## Come funziona

- `alchemy.run.ts` — stack: `Cloudflare.R2.Bucket("BustePaga")` (privato,
  nessun accesso pubblico) + Worker.
- `src/worker.ts` — Worker Effect-native. Init: binding browser + bucket,
  secrets via `Config.Redacted` (al deploy diventano `secret_text`),
  `Cloudflare.Workers.cron("0 6 27 * *", ...)`.
- `src/zucchetti.ts` — il flusso con puppeteer: login → menu "Aree
  applicative" → Workspace → MySpace → "I miei documenti" → click sulla
  riga `Libro unico di MM-YYYY` del mese corrente → lettura dell'URL del PDF
  viewer → `fetch` autenticato → bytes.
- Guard: se il PDF scaricato è byte-identico a quello del mese precedente,
  l'email di conferma segnala il sospetto (noto problema del portale: può
  servire lo stesso documento per righe diverse).
- Email — notifica via **Email Service nativo Cloudflare**: `alchemy.run.ts`
  crea il `SendingSubdomain` (MX/SPF/DKIM/DMARC automatici) e la `Address`
  destinataria (da verificare con un click all'ultimo deploy).

## Vendoring

`vendor` pina i sorgenti per SHA in `vendor/` (`vendors.lock`):

- `effect` → tag `effect@4.1.4`, subdir `packages/effect`
- `alchemy-repo` → tag `v2.0.0-beta.87` (monorepo completo)

I package.json del monorepo alchemy non sono installabili così come sono
(`workspace:*`, `catalog:`). `scripts/sync-vendor.mjs` (run dal
`preinstall`) li riscrive in `vendor/` con versioni concrete: la chiusura
dei pacchetti interni `@alchemy.run/*` parte da alchemy + cloudflare e
segue sia gli import TS sia le deps `workspace:`. Applica anche le patch ai
sorgenti (`applyVendorPatch`, testato, idempotente).

Dopo `vendor update`: rilancia `npm install` (il preinstall ri-patcha tutto)
e ricontrolla `tsc` — le patch possono non applicarsi più.

Nota TS: serve **TypeScript 6.0.3** (il sorgente effect 4.1.4 non passa
5.9; alchemy internamente usa tsgo 7.x).

## Limiti noti

- Se Zucchetti introduce OTP/SSO sul login, l'automazione si rompe.
- I selettori sono legati al layout attuale del portale HR Progest
  (verificato a novembre 2026).
- La busta del mese è cercata con il nome `Libro unico di MM-YYYY Mensili`;
  se il datore di lavoro la rinomina, va aggiornato `monthLabel`.
