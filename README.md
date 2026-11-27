# zucchetti

Payslips from the Zucchetti HR portal, archived and browsable from the macOS menu bar.

| | |
|---|---|
| `apps/worker` | Cloudflare Worker: scrapes the portal on the 27th, stores PDFs in R2, emails them, and serves `GET /buste`, `GET /buste/:key`, `POST /run`, `GET /status/:id` behind the `x-run-token` header. See its README. |
| `apps/desktop` | Menu bar app (Go, [MyGo](https://github.com/egoist/mygo) native UI): list, download, re-fetch and Quick Look payslips, and read leave balances from the PDFs. A PTO calendar tab is stubbed in. |

```sh
mise //apps/desktop:dev      # live-reload the app
mise //apps/desktop:build    # Zucchetti.app + .dmg
mise //apps/worker:deploy    # needs apps/worker/.env, see apps/worker/README.md
mise '//...:test'            # every project
mise :test                   # the project of the current directory
mise tasks --all             # all tasks
```

Managed with [mise monorepo tasks](https://mise.jdx.dev/tasks/monorepo.html): each app's `mise.toml` has its own tools (Go for the app, Node for the worker) and tasks.

In the app, open Settings (gear, or Cmd+,) and set the Worker URL and token.
