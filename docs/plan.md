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
            │
Phase 5  ┌──────────┬──────────┬──────────┬──────────┐
         5A Orders  5B Pay    5C Admin   5D Kit     (parallel)
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

## Phase 5: UX fixes from the e2e review

Found by a Playwright pass on a 390×844 touch viewport (2026-11-20). The numbers in brackets are the finding numbers from that review. Almost every order fix touches `orders.go` and `orders.templ`, so 5A is a single step done in order, not split in parallel. 5B, 5C and 5D own separate files and run beside it.

### 5A Orders (sequential, one branch)

Owns `internal/web/orders.go`, `orders.templ`, `orders_test.go`, `internal/db/orders.go`, and the new files `internal/web/customer.go` and `customer.templ`.

| # | Fix | Work | Done when |
|---|-----|------|-----------|
| 5A.1 | Empty order sent to kitchen leads to a dead-end page [2] | `statusForm`: add `hx-post`, `hx-target="#acoes"`, `hx-disabled-elt="find button"`. `fail` already puts errors in `#acoes-erro`, and `redirect` already sends `HX-Redirect` on success. Disable "Enviar à cozinha" when `len(o.Items) == 0`, with the hint "Adicione itens para enviar.", the same way "Fechar" shows its hint. Keep the plain POST for no-JS. | Test: an htmx POST of `para=cozinha` on an empty order returns 200 with `acoes-erro` filled, and the status is unchanged. The button renders disabled when there are no items. |
| 5A.2 | Minus at qty 1 deletes with no confirm [4] | `orderItems`: at `qty == 1`, swap the "−" button for a remove button ("Remover", trash icon) in its own form with `hx-delete` and `hx-confirm="Remover <nome>?"`, plus `onsubmit="return confirm(...)"` for no-JS. | Test: PATCH `qty=0` still removes the item (the server behavior stays). The template renders the remove button only at qty 1. |
| 5A.3 | No note per item [5] | New `(*DB).SetItemNote(ctx, orderID, itemID, note)`, allowed only while the order is `aberto`, max 100 runes. In `setItemQty`, accept an optional `nota` field (PATCH with `nota` and no `qty` changes only the note). UI: tapping the item name opens a `<details>` with a one-line input and a "Salvar" button. The note already renders under the name, and the ticket and messages already print `it.Note`; check both. | Tests: set, clear, too long, and refused when not `aberto`. The note appears on the ticket text. |
| 5A.4 | Name, phone and note can't be edited after create [6] | New feature file `customer.go` with `registerCustomer(mux)`: `POST /pedidos/{id}/cliente`, fields `nome`, `telefone`, `obs`. Pull the name, phone and note checks out of `parseNewOrder` into `checkCustomer` and reuse it in both. New `(*DB).SetCustomer`. UI: an "Editar cliente" button in the actions sheet opens a `cliente-sheet`, following the pattern of `ConsumePanel` and `consumo-salvo`. Allowed in any status except `fechado` and `cancelado`. | Tests: valid edit, empty name refused, bad phone refused, refused on `fechado`. |
| 5A.5 | Home list never refreshes [3] | In `OrderListPage`, wrap the cards in `<div id="lista" hx-get="/" hx-trigger="every 15s [document.visibilityState=='visible']" hx-select="#lista" hx-swap="outerHTML">`. `orderList` stays as it is, since `hx-select` cuts the fragment out on the client. | A second browser shows a new order within 15 s without a reload. |
| 5A.6 | Home card has no payment status [9] | On the card, show a small "Não pago" chip (or "Falta R$ x") when the order is not `aberto` and not `Paid()`. Use `RemainingCents`. Check that `ListOpenOrders` loads payments, and add the sum if it doesn't. | Test: the list HTML contains "Não pago" for an unpaid order in `pronto`. |
| 5A.7 | Add sheet closes after every item [7] | In `addItem`, stop sending the `item-added` trigger. Send `item-added-keep` instead, which swaps `#add-step` back to the category step (`AddStepHX` with "Adicionar item") and keeps the sheet open. Show the toast as now. The user closes the sheet with ✕. | Adding two items in a row needs no reopen. The list behind the sheet updates. |
| 5A.8 | Address error is far from the field [8] | `NewOrderPage`: pass the failing field name with the error (`inputError` gets a field, or `parseNewOrder` returns it). Render the message under that field, add `aria-invalid="true"` and a red border, and `autofocus` it. | Test: posting entrega with no address renders the error next to `endereco` with `aria-invalid`. |
| 5A.9 | Phone pattern shows a generic browser message [12] | Add `title="4 dígitos, ex.: 1234"` to the phone input. | Visible in the HTML. |
| 5A.10 | Copy fallback gives no hint [10] | `copyScript`: when it falls back to the selected textarea, show a toast or a line under it: "Toque e segure para copiar". | Without `navigator.clipboard`, the hint is visible. |
| 5A.11 | Direct `/adicionar` page header loses the step name [12] | `addItemStep` full-page path: pass the step title (product or category name) to `AddItemPage`, and give its `<h1>` an id so `sheetTitle` also updates it on htmx steps. | Opening `/pedidos/1/adicionar?produto=8` shows the product name. |
| 5A.12 | Admin link tap target is 20px; red focus ring [12] | Home "Admin" link: `min-h-12 inline-flex items-center px-3`. `fieldClass`: the same neutral focus ring as 5D. | The link is 48px tall. A focused field is not red. |

### 5B Payment split kept between visits [1] (parallel)

Owns `internal/web/payment.go`, `payment.templ`, `payment_test.go`, `internal/db/payments.go`, `migrate.go`, `schema.sql`.

Inside one open sheet the split is already kept (`paymentDone` passes `people` back). It is lost when the sheet is reopened after a reload, or on another phone, because the panel starts at 1.

- New migration step: `ALTER TABLE orders ADD COLUMN split_people INTEGER NOT NULL DEFAULT 1`. Do not edit shipped steps.
- `AddPayment` takes `people` and stores it on the order in the same transaction. `Order` gets `SplitPeople`.
- `PaymentPanel` starts at `o.SplitPeople` instead of 1. `splitScript` still fills one `EqualShare`, capped at what is left.
- Show "Parte 2 de 3" next to "Valor a pagar" when `SplitPeople > 1`.

**Done when:** the test splits R$ 120 three ways and pays one part. A fresh GET of the order then renders `pessoas=3` and `valor=40,00`. Money stays in integer centavos.

### 5C Admin screens (parallel)

Owns `internal/web/auth.templ`, `report.templ`, `menu.templ`, `report_test.go`.

- Login page: add a "← Pedidos" link to `/` [11].
- Report: "1 pedido" / "N pedidos" [12].
- "Cardápio" and "Relatório" header links: 48px tap target, same classes as 5A.12 [12].

**Done when:** the report test checks the singular form, and the login page links home.

### 5D UI kit (parallel)

Owns `internal/web/components.templ` and `web/input.css`.

- Focus on fields looks like an error [12]: change `focus:border-brand focus:ring-brand-soft` to a neutral ink ring in `MoneyInput`. Red stays only for `aria-invalid` and errors. 5A.12 makes the same change in `fieldClass` (`orders.templ`).
- Toast covers the order header [12]: move `ToastRegion` and `Toast` to the bottom, above the fixed action bar (`bottom-[calc(7rem+env(safe-area-inset-bottom))]`).

**Done when:** a focused field is not red, and the toast does not cover the header on `/pedidos/{id}`.

### Phase 5 check

Rerun the Playwright scripts from the review against a seeded temp DB with `PRINT_PREVIEW=1`, in light and dark mode. `make test` and `go vet ./...` are clean.

## Running steps in parallel

- Each step runs in its own git worktree and branch, for example `phase1/1a-db`.
- A step edits only the files in its "Owns files" column. A change needed in a shared file, such as `main.go` or `layout.templ`, is reported back, not made by the step.
- Generated files (`*_templ.go`, `static/app.css`) are never committed, so they never conflict.
- After a phase, merge the branches, run `make gen test`, then start the next phase.
