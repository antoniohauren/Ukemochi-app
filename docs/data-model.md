# Data model

SQLite. All money is stored as integer centavos (`R$ 12,50` = `1250`). Timestamps are stored as UTC text in ISO 8601.

## Menu

A category has items. An item has one or more products. A product is the thing that is sold and priced.

```
categories 1──* items 1──* products
```

Example:

| Table      | Row                                                          |
|------------|--------------------------------------------------------------|
| categories | `Refrigerante`                                               |
| items      | `Coca-Cola`                                                  |
| products   | variant `Zero`, size `Lata`, printable `COCA ZERO LT`, `600` |

An item with no variants has one product with empty `variant` and `size`.

```sql
CREATE TABLE categories (
  id       INTEGER PRIMARY KEY,
  name     TEXT NOT NULL UNIQUE,
  position INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE items (
  id          INTEGER PRIMARY KEY,
  category_id INTEGER NOT NULL REFERENCES categories(id),
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  active      INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE products (
  id             INTEGER PRIMARY KEY,
  item_id        INTEGER NOT NULL REFERENCES items(id),
  variant        TEXT NOT NULL DEFAULT '',
  size           TEXT NOT NULL DEFAULT '',
  printable_name TEXT NOT NULL,
  price_cents    INTEGER NOT NULL CHECK (price_cents >= 0),
  active         INTEGER NOT NULL DEFAULT 1
);
```

Menu rows are never deleted, only set to `active = 0`. Old orders keep pointing to them.

## Orders

```sql
CREATE TABLE orders (
  id               INTEGER PRIMARY KEY,
  channel          TEXT NOT NULL CHECK (channel IN ('whatsapp','balcao')),
  consume          TEXT NOT NULL CHECK (consume IN ('entrega','local')),
  client_name      TEXT NOT NULL,
  phone_last4      TEXT,
  status           TEXT NOT NULL DEFAULT 'aberto'
                   CHECK (status IN ('aberto','cozinha','pronto','saiu','fechado','cancelado')),
  notes            TEXT NOT NULL DEFAULT '',
  address          TEXT NOT NULL DEFAULT '',  -- required for entrega (migration 2)
  split_people     INTEGER NOT NULL DEFAULT 1, -- migration 4
  expected_method  TEXT CHECK (expected_method IN ('pix','credito','debito','dinheiro')), -- how what is left will be paid (migration 5)
  change_for_cents INTEGER CHECK (change_for_cents > 0), -- dinheiro only: the note the client pays with (migration 5)
  created_at       TEXT NOT NULL,
  closed_at        TEXT
);

CREATE TABLE order_items (
  id               INTEGER PRIMARY KEY,
  order_id         INTEGER NOT NULL REFERENCES orders(id),
  product_id       INTEGER NOT NULL REFERENCES products(id),
  qty              INTEGER NOT NULL CHECK (qty BETWEEN 1 AND 999),
  unit_price_cents INTEGER NOT NULL,
  name_snapshot    TEXT NOT NULL,
  note             TEXT NOT NULL DEFAULT '',
  split_parts      INTEGER NOT NULL DEFAULT 1 CHECK (split_parts BETWEEN 1 AND 20) -- parts per unit when shared, Por Consumo (migration 7)
);

-- Payment parts (migration 3, decision 009). One row pays the whole bill; several split it.
CREATE TABLE order_payments (
  id               INTEGER PRIMARY KEY,
  order_id         INTEGER NOT NULL REFERENCES orders(id),
  method           TEXT NOT NULL CHECK (method IN ('pix','credito','debito','dinheiro')),
  amount_cents     INTEGER NOT NULL CHECK (amount_cents >= 0),
  cash_given_cents INTEGER CHECK (cash_given_cents >= amount_cents),  -- dinheiro only, optional
  paid_at          TEXT NOT NULL
);

-- Units of each line a payment part covers when paid by consumption (migration 6).
CREATE TABLE order_payment_items (
  payment_id    INTEGER NOT NULL REFERENCES order_payments(id) ON DELETE CASCADE,
  order_item_id INTEGER NOT NULL REFERENCES order_items(id) ON DELETE CASCADE,
  qty           INTEGER NOT NULL CHECK (qty >= 1),
  PRIMARY KEY (payment_id, order_item_id)
);

CREATE INDEX orders_created_at ON orders(created_at);
CREATE INDEX order_items_order ON order_items(order_id);
CREATE INDEX order_payments_order ON order_payments(order_id);
```

`unit_price_cents` and `name_snapshot` are copied from the product when the item is added. Later menu changes don't alter past orders or reports.

The order total is computed from `order_items` (`SUM(qty * unit_price_cents)`), not stored. What is paid is `SUM(order_payments.amount_cents)`; an order is paid when that equals the total.

Migration 3 moved each order's old `payment`, `paid_at` and `cash_given_cents` into one `order_payments` row for the order's total, then dropped those columns.

## Daily report query

```sql
SELECT op.method, COUNT(DISTINCT o.id) AS orders,
       SUM(op.amount_cents) AS total_cents
FROM orders o JOIN order_payments op ON op.order_id = o.id
WHERE o.status = 'fechado' AND o.closed_at >= ? AND o.closed_at < ?
GROUP BY op.method;
```

The day's order count and total come from the per-channel query over `order_items`, since a split order shows up under more than one method.

The bounds are the start and end of the local day (`America/Sao_Paulo`) converted to UTC.

## Migrations

One embedded `schema.sql` with `PRAGMA user_version`. On start-up, the app applies each numbered step above the current version inside a transaction.

## Connection settings

`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`
