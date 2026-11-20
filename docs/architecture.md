# Architecture

A single Go binary serves HTML rendered with templ. htmx swaps page fragments, so there is no frontend build step. Static files and the SQL schema are embedded with `embed.FS`.

```
phone (PWA) ──HTTPS──> Caddy ──HTTP──> ukemochi (Go) ──> ukemochi.db (SQLite)
```

## Layout

```
cmd/ukemochi/main.go   config from env, DB open, routes, server
internal/db/           schema.sql, migrations, hand-written queries (database/sql)
internal/web/          handlers, templ views, admin PIN middleware
internal/money/        centavos formatting and change calculation
web/input.css          Tailwind entry: `@import "tailwindcss"; @source "../internal/web";` plus theme tokens
static/                htmx.min.js, app.css (generated), print.css, sw.js, manifest.webmanifest, icons
deploy/                Caddyfile, ukemochi.service
docs/                  this documentation
```

## Dependencies

| Module                  | Why                                 |
|-------------------------|-------------------------------------|
| `github.com/a-h/templ`  | Type-safe HTML templates            |
| `modernc.org/sqlite`    | SQLite driver without cgo           |
| htmx (one JS file)      | Partial page updates                |
| Tailwind CSS v4 (build tool only) | Utility CSS. The standalone CLI compiles `web/input.css` to `static/app.css`, so no Node is needed and nothing ships at runtime. |

Nothing else. Routing, sessions, the HMAC cookie and message formatting all use the stdlib.

## Routes

Go 1.22 `http.ServeMux` patterns.

| Method & path                       | Purpose                                 |
|-------------------------------------|-----------------------------------------|
| `GET /`                             | Open orders (cards with status)         |
| `GET /pedidos/novo`                 | New order form                          |
| `POST /pedidos`                     | Create order                            |
| `GET /pedidos/{id}`                 | Order editor                            |
| `GET /pedidos/{id}/adicionar`       | Add-item step: categories, `?categoria=` items, `?item=` variants, `?produto=` quantity |
| `POST /pedidos/{id}/itens`          | Add item with `qty` (htmx fragment)     |
| `PATCH /pedidos/{id}/itens/{item}`  | Change qty or note                      |
| `DELETE /pedidos/{id}/itens/{item}` | Remove item                             |
| `POST /pedidos/{id}/status`         | Move to the next status                 |
| `POST /pedidos/{id}/pagamento`      | Record payment method and cash given    |
| `GET /pedidos/{id}/troco?valor=`    | Change calculation (htmx fragment)      |
| `GET /pedidos/{id}/mensagem/{tipo}` | Message text: `confirmacao`, `pronto`, `saiu` |
| `GET /pedidos/{id}/imprimir`        | 58mm ticket page                        |
| `POST /pedidos/{id}/imprimir`       | Print on the Bluetooth printer (falls back to the ticket page) |
| `GET /admin/login`, `POST /admin/login` | PIN login                           |
| `GET /admin/relatorio?dia=`         | Daily report                            |
| `/admin/cardapio/...`               | Menu CRUD                               |

Status transitions are checked on the server. See the rules in [flows](flows.md).

## Screens

- **Order list:** open orders, newest first, with status color and total. Large **Novo pedido** button.
- **Order editor:** category tabs, then a product grid. Tap to add. The current order sits in a bottom sheet with quantities, total and actions.
- **Payment:** method buttons. When **Dinheiro** is chosen, a numeric field shows the change live.
- **Admin report:** one day, with counts and totals per payment method and channel, the top items, and a **Copiar resumo** button.

## Copy to clipboard

`navigator.clipboard.writeText()` in a small inline script. It needs HTTPS, see [deployment](deployment.md). If it fails, a `<textarea>` with the selected text is shown instead.

## Printing

With `PRINTER_BT` set to a printer's Bluetooth address, the server prints the ticket itself: `POST /pedidos/{id}/imprimir` and sending an order to the kitchen open an RFCOMM connection (channel 1), send ESC/POS text and disconnect (`internal/btprint`, Linux only). Text goes out in code page CP860 (`ESC t 3`), so accents print. Pair and trust the printer on the server once (`bluetoothctl pair <mac>`, `trust <mac>`). Do not "connect" it in the desktop Bluetooth menu: a serial-only printer drops that link after a moment, which is normal.

Without `PRINTER_BT`, or when the printer does not answer on kitchen send, `/pedidos/{id}/imprimir` is a standalone page that calls `window.print()` on load. The phone sends it to the printer through the Android print service (for example RawBT or the printer vendor's app).

```css
@page { margin: 0; }                      /* static/print.css */
@page { size: 58mm 49.6mm; }              /* inline, height from the line count */
body  { width: 48mm; margin: 0 auto; padding: 4mm 0; font: 2.4mm/1.2 monospace; }
```

`size: 58mm auto` is invalid CSS, so browsers drop it and print on A4. The page instead sets the height to 2.9mm per line plus 10mm (`ticketHeight` in `print.go`), so a saved PDF is one 58mm strip.

58mm paper has about 48mm printable width, or 32 characters in monospace. The ticket shows the order number, time, client name, channel and consume type, items as `qty x printable_name`, notes, total and payment status.

## PWA

- `manifest.webmanifest`: name, icons (192 and 512), `display: standalone`, `start_url: /`.
- `sw.js`: caches only the static files. Order pages always come from the network, so stale data is never shown.

## Auth

Order screens are open to anyone on the shop network. `/admin/*` needs a session cookie, set after the correct `ADMIN_PIN` is entered. The cookie value is `expiry|HMAC-SHA256(SESSION_KEY, expiry)`, with `HttpOnly`, `Secure` and `SameSite=Strict`. Login attempts are rate-limited in memory.

## Testing

- `internal/money`: formatting and change calculation, table tests.
- Status transitions: close unpaid fails, `saiu` is refused for `local`.
- Daily report query against an in-memory SQLite database.
