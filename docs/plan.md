# Implementation plan

The work is split into phases. Steps inside one phase touch separate files and can run in parallel. A phase starts only after the previous one is merged.

```
Phase 0  Scaffold (1 step, sequential)
            │
Phase 1  ┌──────┬──────┬──────┬──────┬──────┐
         1A DB  1B $  1C UI  1D PWA 1E Ops  (parallel)
            │
Phase 2  ┌──────────┬──────────┬──────────┐
         2A Orders  2B Menu   2C Print    (parallel)
            │
Phase 3  ┌──────────┬──────────┐
         3A Payment 3B Report             (parallel)
            │
Phase 4  Integration and test pass (sequential)
```

## Phase 0: Scaffold

Done by one agent because every later step builds on it.

- `go mod init`, add `github.com/a-h/templ` and `modernc.org/sqlite`.
- `cmd/ukemochi/main.go`: env config, open DB, `http.ServeMux`, serve embedded `static/`.
- `internal/web/layout.templ`: base HTML with `<meta viewport>`, `app.css`, `htmx.min.js`, manifest link, `lang="pt-BR"`.
- `web/input.css` with `@import "tailwindcss";` and `@source "../internal/web";`.
- `Makefile` targets: `gen` (templ + tailwind), `run`, `test`, `build-arm64`.
- `.gitignore`: `bin/`, `*.db`, `*_templ.go`, `static/app.css`.
- **Contract for later phases:** the `DB` struct in `internal/db`, the `Server` struct holding it in `internal/web`, and a route-registration function per feature file (`registerOrders(mux)`, ...). Each feature then owns its own file, so parallel steps don't conflict.

**Done when:** `make gen run` serves an empty Tailwind-styled home page.

## Phase 1: Foundations (parallel)

| Step | Owns files | Work | Done when |
|------|-----------|------|-----------|
| 1A DB | `internal/db/schema.sql`, `migrate.go`, `menu.go`, `orders.go` | Schema from [data model](data-model.md), `user_version` migrations, PRAGMAs, query functions for menu and orders, status-transition check in `orders.go` | Tests pass on `:memory:`. Closing an unpaid order fails. `saiu` is refused for `local`. |
| 1B Money | `internal/money/` | `Format(cents) "R$ 1.234,56"`, `Parse("12,50")`, `Change(total, given)` | Table tests pass |
| 1C UI kit | `internal/web/components.templ`, `web/input.css` | Theme tokens, button, card, status chip, bottom sheet, numeric input, toast. Mobile first, large tap targets. | Demo page at `/dev/ui` renders all components |
| 1D PWA | `static/manifest.webmanifest`, `static/sw.js`, `static/icons/`, `static/htmx.min.js` | Manifest, static-only cache service worker, vendored htmx with pinned version | Lighthouse marks the app installable on localhost |
| 1E Ops | `deploy/`, `internal/web/auth.go` | Caddyfile, systemd unit, backup cron. Admin PIN login, HMAC cookie, rate limit, `requireAdmin` middleware | Auth tests: a wrong PIN is refused, a tampered cookie is refused |

## Phase 2: Core features (parallel)

All depend on 1A, 1B and 1C.

| Step | Owns files | Work |
|------|-----------|------|
| 2A Orders | `internal/web/orders.go`, `orders.templ`, `messages.go` | Order list, new order form, editor (category tabs, add/remove/qty via htmx), status buttons, the three copy messages with clipboard and a `<textarea>` fallback |
| 2B Menu admin | `internal/web/menu.go`, `menu.templ` | CRUD for categories, items and products behind `requireAdmin`, deactivate instead of delete, seed script with sample menu |
| 2C Print | `internal/web/print.go`, `print.templ`, `static/print.css` | 58mm ticket page, auto `window.print()`, plain CSS without Tailwind, 32-character lines |

## Phase 3: Money features (parallel)

| Step | Owns files | Work |
|------|-----------|------|
| 3A Payment | `internal/web/payment.go`, `payment.templ` | Payment method buttons, cash field with live change via `hx-get .../troco`, mark paid, close button enabled only when paid |
| 3B Report | `internal/web/report.go`, `report.templ`, `internal/db/report.go` | Daily report by payment method and channel, top items, day picker, **Copiar resumo** text, São Paulo day boundaries |

## Phase 4: Integration

- Walk the three flows in [flows](flows.md) on a phone-sized browser.
- Print a real ticket on the 58mm printer from Android.
- Deploy behind Caddy, install the PWA, confirm the clipboard works over HTTPS.
- `go test ./...` and `go vet ./...` are clean.

## Running steps in parallel

- Each step runs in its own git worktree and branch, for example `phase1/1a-db`.
- A step edits only the files in its "Owns files" column. A change needed in a shared file, such as `main.go` or `layout.templ`, is reported back, not made by the step.
- Generated files (`*_templ.go`, `static/app.css`) are never committed, so they never conflict.
- After a phase, merge the branches, run `make gen test`, then start the next phase.
