# Decisions

Short records of choices made and why.

## 001: Browser printing for the 58mm printer

Use `window.print()` with a 58mm `@page` print stylesheet.

- **Why:** this works with any printer the phone's OS can see, over USB, Bluetooth or the network, and it needs no driver code.
- **Trade-off:** a print dialog appears on each print, and layout control is weaker than raw ESC/POS.
- **Revisit:** if the print dialog slows the waitresses down, add ESC/POS printing over TCP 9100 from the server.

## 002: In-house server with a real certificate over DNS-01

Run on a machine in the restaurant, behind Caddy, with a Cloudflare DNS-01 certificate for a LAN IP.

- **Why:** the PWA and clipboard need HTTPS, and orders keep working without internet. There is no hosting cost and no open ports.
- **Trade-off:** the owner maintains a machine and its backups. Some routers block private-IP DNS answers.

## 003: Single admin PIN

Order screens are open on the LAN. Reports and menu editing need a PIN.

- **Why:** a small team, one shared device flow, and no account management.
- **Trade-off:** orders don't record which waitress created them.
- **Revisit:** if they need per-waitress tracking.

## 004: `modernc.org/sqlite` over Turso or a cgo driver

- **Why:** pure Go, so the build cross-compiles to a Raspberry Pi with `CGO_ENABLED=0`. A local file needs no service. Turso would add a network dependency.

## 005: Money as integer centavos

- **Why:** floats round wrong (`0.1 + 0.2`). Integers are exact, and they only need formatting at the edges.

## 006: Snapshot price and name in `order_items`

- **Why:** menu edits must not change past orders or reports.
- **Exception:** the ticket and the WhatsApp confirmation use the current `products.printable_name`, so a reprint shows a renamed product's new short name. Prices and totals still come from the snapshot.

## 007: Minimal dependencies

Only templ, the SQLite driver, htmx and Tailwind (build time only).

- **Why:** this was a project rule. Fewer updates and less to audit.

## 008: Tailwind CSS through the standalone CLI

- **Why:** fast to build mobile-first UI with utility classes in templ files. The standalone binary needs no Node or npm, and the output is one static CSS file.
- **Trade-off:** the build has an extra step, and `static/app.css` must be regenerated after class changes.
- **Rule:** the print ticket uses its own `print.css`, not Tailwind, so the 58mm layout stays predictable.

## 009: Split bills as payment parts

An order's payment is a list of parts in `order_payments`, each with its own method and amount. The amount defaults to what is left, a typed amount pays part of it, and the **Dividir entre** people picker fills an equal share (`ceil(total / N)`, the last person pays what is left).

- **Why:** tables often split the bill, by person or by method (part PIX, part cash). One method per order could not record that, and the report would put the whole total under one method.
- **Rule:** a part can't exceed what is left, and an order closes only when its parts add up to the total exactly. So the report's per-method totals always add up to the day's total.
- **Trade-off:** a split order counts once under each method it used, so the per-method order counts can add up to more than the number of orders.

## 010: Windows tray icon through raw Win32 calls

On Windows the app adds a notification-area icon (`internal/tray`): **Abrir no navegador** (also a double-click), **Abrir log** and **Sair**, which shuts down the same way as Ctrl+C. On other systems it does nothing.

The built `ukemochi.exe` is a GUI program (`-ldflags=-H=windowsgui`), so it opens no console window. With no console it has no stderr, so the log goes to `ukemochi.log` beside the exe (or `LOG_PATH`). At startup a log over 5 MB is moved to `ukemochi.log.old`, so the two files stay under about 10 MB.

- **Why:** on a restaurant PC the app is started by a double-click. A console window is easy to close by mistake, which stops the app, and it has no way to open the app. The tray is the only thing on screen.
- **Rule:** it uses `syscall` with user32, shell32 and kernel32 directly, no tray module (decision 007) and no cgo. The icon is the PWA icon PNG, which Windows reads as icon data.
- **Trade-off:** run from a terminal, the built exe returns at once and prints nothing: read the log file instead. `go run` and `make run` stay console programs. A console build still gets the tray, with **Mostrar/Ocultar console** when it owns its console (double-click start), never hiding a terminal it was started from.
