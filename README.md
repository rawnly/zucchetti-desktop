# zucchetti

Payslips from the Zucchetti HR portal, archived and browsable from the macOS menu bar.

| | |
|---|---|
| `apps/worker` | Cloudflare Worker: scrapes the portal on the 27th, stores PDFs in R2, emails them, and serves `GET /buste`, `GET /buste/:key`, `POST /run`, `GET /status/:id` behind the `x-run-token` header. See its README. |
| `apps/desktop` | Menu bar app (Go, [MyGo](https://github.com/egoist/mygo) native UI): list, download, re-fetch and Quick Look payslips, and read leave balances from the PDFs. A PTO calendar tab is stubbed in. |

```sh
mise run desktop:dev      # live-reload the app
mise run desktop:build    # Zucchetti.app + .dmg
mise run worker:deploy    # needs apps/worker/.env, see apps/worker/README.md
mise run test
```

In the app, open Settings (gear, or Cmd+,) and set the Worker URL and token.
