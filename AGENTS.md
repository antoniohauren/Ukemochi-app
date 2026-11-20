# AGENTS.md

Guide for coding agents working on Ukemochi: a Go + templ + htmx + SQLite order app for a small restaurant, used on phones over the LAN.

Read `docs/` before larger changes: `architecture.md` (layout, routes, screens), `data-model.md`, `flows.md` (status rules), `decisions.md` (why things are the way they are), `plan.md` (phases).

## Commands

| Command            | What it does                                                        |
|--------------------|---------------------------------------------------------------------|
| `make gen`         | `go tool templ generate`, then Tailwind builds `static/app.css`. Downloads Tailwind and htmx on first run. |
| `make run`         | `gen`, then `go run ./cmd/ukemochi`                                 |
| `make test`        | `gen`, then `go test ./...`                                         |
| `make build-arm64` | Static `CGO_ENABLED=0` build for the Raspberry Pi                   |

Env: `ADDR` (default `127.0.0.1:8080`), `DB_PATH` (default `ukemochi.db`).

Run `make gen` after editing any `.templ` file or Tailwind classes. Generated files (`*_templ.go`, `static/app.css`, `static/htmx.min.js`) are not committed.

## Rules

- **Dependencies:** only templ, `modernc.org/sqlite`, htmx and Tailwind (build time). Use the stdlib for everything else: routing, sessions, cookies, formatting. Do not add modules (decision 007).
- **No cgo.** The build must cross-compile with `CGO_ENABLED=0`.
- **No JS framework, no Node.** htmx plus small inline scripts only.

## Go

- `cmd/ukemochi/main.go` wires config, DB and routes. Keep logic out of it.
- `internal/web`: handlers and templ views. Each feature lives in its own file with a `register<Feature>(mux)` method on `Server`, called from `Routes`. This keeps parallel work in separate files.
- Routes use Go 1.22+ `http.ServeMux` patterns (`"POST /pedidos/{id}/itens"`, `r.PathValue("id")`). Route paths and UI text are in Portuguese; Go identifiers are in English.
- Validate all form and path input in the handler. Check status transitions on the server, never only in the UI.
- Return errors up; log them once at the handler. Do not panic in handlers.
- Format with `gofmt`. Pass `go vet ./...`.

## templ + htmx

- Views are `.templ` files in `internal/web`. Render with `Component.Render(r.Context(), w)`.
- A full page uses the layout. An htmx request (`HX-Request: true` header) gets only the fragment it swaps.
- Prefer `hx-get`/`hx-post`/`hx-patch`/`hx-delete` with `hx-target` and `hx-swap` over custom JS.
- Forms must still post without htmx where practical (normal `action`/`method`).
- Mobile first: large tap targets, one-hand use. Tailwind utility classes in templ files.
- The 58mm print ticket uses `static/print.css`, not Tailwind (decision 008).

## SQLite

- `internal/db` owns the schema, migrations and all SQL. Handlers never write SQL.
- Hand-written queries with `database/sql`, added as methods on `*db.DB` in feature files. No ORM, no query builder.
- Always use `?` placeholders. Never build SQL with string formatting from input.
- Pass `ctx` (`QueryContext`, `ExecContext`). Wrap multi-statement writes in a transaction.
- The pool has one connection (`SetMaxOpenConns(1)`) with WAL, `foreign_keys` and `busy_timeout`. Do not hold a `*sql.Rows` open while running another query: it deadlocks. Read rows into a slice, close, then query again.
- Schema changes go through a migration. Never edit a migration that has shipped.
- Money is integer centavos (`int64`). No floats anywhere, format only at the edges (decision 005).
- `order_items` snapshots product name and price, so menu edits never change past orders (decision 006).

## Tests

- `go test ./...`. Use a temp DB via `db.Open(filepath.Join(t.TempDir(), "test.db"))`, not mocks.
- Test handlers with `httptest`. Cover status transitions, money math and input validation.

## Commits

Conventional Commits (`feat:`, `fix:`, `docs:`). Small, one concern each.
