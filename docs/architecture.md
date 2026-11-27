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
internal/tray/         Windows notification-area icon (no-op elsewhere)
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
| `GET /?status=&consumo=`            | Open orders, filtered by status (`aberto`, `cozinha`, `pronto`, `saiu`) and consume (`entrega`, `local`); empty = all. A plain `/` reuses the last filter (cookie, 12h) |
| `GET /pedidos/fechados?dia=`         | One day's fechado and cancelado orders (cancelado counts on its creation time), newest first; empty `dia` = today |
| `GET /pedidos/novo`                 | New order form                          |
| `POST /pedidos`                     | Create order                            |
| `GET /pedidos/{id}`                 | Order editor                            |
| `GET /pedidos/{id}/adicionar`       | Add-item step: categories, `?categoria=` items, `?item=` variants, `?produto=` quantity |
| `POST /pedidos/{id}/itens`          | Add item with `qty` (htmx fragment)     |
| `PATCH /pedidos/{id}/itens/{item}`  | Change qty or note                      |
| `DELETE /pedidos/{id}/itens/{item}` | Remove item                             |
| `POST /pedidos/{id}/status`         | Move to the next status; `para=cozinha` prints the kitchen ticket |
| `POST /pedidos/{id}/pagamento`      | Add a payment part: `metodo`, `valor` (empty = all that is left), `recebido` (cash) |
| `DELETE /pedidos/{id}/pagamentos/{pagamento}` | Remove a payment part (`POST` without htmx) |
| `POST /pedidos/{id}/pagamento-previsto` | Expected payment of what is left: `previsto` (empty clears), `troco_para` (cash) |
| `POST /pedidos/{id}/consumo`        | Switch entrega / local, with the address |
| `GET /pedidos/{id}/troco?valor=&recebido=` | Change calculation (htmx fragment) |
| `GET /pedidos/{id}/mensagem/{tipo}` | Message text: `confirmacao`, `pronto`, `saiu` |
| `GET /pedidos/{id}/imprimir`        | 58mm ticket page                        |
| `POST /pedidos/{id}/imprimir`       | Print on the Bluetooth printer (falls back to the ticket page) |
| `GET /admin/login`, `POST /admin/login` | PIN login                           |
| `GET /admin/relatorio?dia=`         | Daily report                            |
| `POST /admin/relatorio/caixa`       | Set the day's caixa inicial (opening cash) |
| `/admin/cardapio/...`               | Menu CRUD                               |

Status transitions are checked on the server. See the rules in [flows](flows.md).

## Screens

- **Order list:** open orders, newest first, with status color and total. Two filter buttons on top, **Status** (Todos, Abertos, Cozinha, Prontos, Saiu) and **Tipo** (Todos, Entrega, No local), each showing its choice and opening a sheet of options with their counts; Entrega + Prontos is what is left to deliver. The 15s refresh waits while a sheet is open.
- **Bottom nav:** on the order lists, report and menu: Abertos, Fechados, a raised **+** (Novo pedido) in the middle, Relatório and a ☰ Menu that opens a sheet with Cardápio and the dark-mode switch.
- **Closed orders:** one day at a time (‹ date ›), fechado and cancelado orders with the same cards as the open list; tap one to see or reopen it. No totals: those are on the admin report.
- **Order editor:** category tabs, then a product grid. Tap to add. The current order sits in a bottom sheet with quantities, total and actions.
- **Payment:** a sheet opened from the actions sheet. It lists the parts paid so far and what is left, then method buttons, a **Dividir entre** people picker (− N +, 1 to 20, like the quantity picker) and the amount, which holds one share (all that is left for 1 person). The picker keeps its value between parts. When **Dinheiro** is chosen, a cash field shows the change live. **Voltar** returns to the actions sheet.
- **Entrega:** a sheet opened from the actions sheet with the address and the **Entrega** / **Consumir no local** buttons. Saving returns to the actions sheet.
- **Admin report:** one day, with counts and totals per payment method and channel, a **Caixa** card (caixa inicial, cash received on closed orders, caixa esperado = the two added), the top items, and a **Copiar resumo** button.

## Copy to clipboard

`navigator.clipboard.writeText()` in a small inline script. It needs HTTPS, see [deployment](deployment.md). If it fails, a `<textarea>` with the selected text is shown instead.

## Printing

With `PRINTER_BT` set to a printer's Bluetooth address, the server prints the ticket itself: `POST /pedidos/{id}/imprimir` and sending an order to the kitchen open an RFCOMM connection (channel 1), send ESC/POS text and disconnect (`internal/btprint`, Linux and Windows). Text goes out in code page CP860 (`ESC t 3`), so accents print. Pair and trust the printer on the server once: on Linux `bluetoothctl pair <mac>`, `trust <mac>`; on Windows pair it in Settings > Bluetooth & devices (Windows uses Winsock `AF_BTH` sockets with the same MAC, so the COM port it creates is not needed). On Windows without make, `.\make.ps1 gen|run|test|seed|build` replaces the Makefile. Do not "connect" it in the desktop Bluetooth menu: a serial-only printer drops that link after a moment, which is normal. With `PRINT_PREVIEW=1` the server logs each ticket's text instead of sending it, for testing without a printer.

Without `PRINTER_BT`, or when the printer does not answer on kitchen send, `/pedidos/{id}/imprimir` is a standalone page that calls `window.print()` on load. The phone sends it to the printer through the Android print service (for example RawBT or the printer vendor's app).

```css
@page { margin: 0; }                      /* static/print.css */
@page { size: 58mm 49.6mm; }              /* inline, height from the line count */
body  { width: 48mm; margin: 0 auto; padding: 4mm 0; font: 2.4mm/1.2 monospace; }
```

`size: 58mm auto` is invalid CSS, so browsers drop it and print on A4. The page instead sets the height to 2.9mm per line plus 10mm (`ticketHeight` in `print.go`), so a saved PDF is one 58mm strip.

58mm paper has about 48mm printable width, or 32 characters in monospace. The ticket shows the order number, time, client name, channel and consume type, items as `qty x printable_name`, notes, total and payment status. An unpaid order with a forma prevista adds `Pagar com` and, for cash, `Troco para` and `Levar troco`.

## PWA

- `manifest.webmanifest`: name, icons (192 and 512), `display: standalone`, `start_url: /`.
- `sw.js`: caches only the static files. Order pages always come from the network, so stale data is never shown.

## Auth

Order screens are open to anyone on the shop network. `/admin/*` needs a session cookie, set after the correct `ADMIN_PIN` is entered. The cookie value is `expiry|HMAC-SHA256(SESSION_KEY, expiry)`, with `HttpOnly`, `Secure` and `SameSite=Strict`. Login attempts are rate-limited in memory.

## Testing

- `internal/money`: formatting and change calculation, table tests.
- Status transitions: close unpaid fails, `saiu` is refused for `local`.
- Daily report query against an in-memory SQLite database.
