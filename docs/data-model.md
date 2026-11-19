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
  payment          TEXT CHECK (payment IN ('pix','credito','debito','dinheiro')),
  paid_at          TEXT,
  cash_given_cents INTEGER,
  notes            TEXT NOT NULL DEFAULT '',
  created_at       TEXT NOT NULL,
  closed_at        TEXT
);

CREATE TABLE order_items (
  id               INTEGER PRIMARY KEY,
  order_id         INTEGER NOT NULL REFERENCES orders(id),
  product_id       INTEGER NOT NULL REFERENCES products(id),
  qty              INTEGER NOT NULL CHECK (qty > 0),
  unit_price_cents INTEGER NOT NULL,
  name_snapshot    TEXT NOT NULL,
  note             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX orders_created_at ON orders(created_at);
CREATE INDEX order_items_order ON order_items(order_id);
```

`unit_price_cents` and `name_snapshot` are copied from the product when the item is added. Later menu changes don't alter past orders or reports.

The order total is computed from `order_items` (`SUM(qty * unit_price_cents)`), not stored.

## Daily report query

```sql
SELECT o.payment, COUNT(DISTINCT o.id) AS orders,
       SUM(oi.qty * oi.unit_price_cents) AS total_cents
FROM orders o JOIN order_items oi ON oi.order_id = o.id
WHERE o.status = 'fechado' AND o.closed_at >= ? AND o.closed_at < ?
GROUP BY o.payment;
```

The bounds are the start and end of the local day (`America/Sao_Paulo`) converted to UTC.

## Migrations

One embedded `schema.sql` with `PRAGMA user_version`. On start-up, the app applies each numbered step above the current version inside a transaction.

## Connection settings

`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;`
