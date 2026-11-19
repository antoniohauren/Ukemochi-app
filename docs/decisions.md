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

## 007: Minimal dependencies

Only templ, the SQLite driver, htmx and Tailwind (build time only).

- **Why:** this was a project rule. Fewer updates and less to audit.

## 008: Tailwind CSS through the standalone CLI

- **Why:** fast to build mobile-first UI with utility classes in templ files. The standalone binary needs no Node or npm, and the output is one static CSS file.
- **Trade-off:** the build has an extra step, and `static/app.css` must be regenerated after class changes.
- **Rule:** the print ticket uses its own `print.css`, not Tailwind, so the 58mm layout stays predictable.
