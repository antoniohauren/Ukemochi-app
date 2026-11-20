-- Migrations. Each step starts with a "-- migrate: N" line and runs once, in order,
-- when PRAGMA user_version is below N. Never edit a step that has shipped; add a new one.

-- migrate: 1
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
  qty              INTEGER NOT NULL CHECK (qty BETWEEN 1 AND 999),
  unit_price_cents INTEGER NOT NULL,
  name_snapshot    TEXT NOT NULL,
  note             TEXT NOT NULL DEFAULT ''
);

CREATE INDEX orders_created_at ON orders(created_at);
CREATE INDEX order_items_order ON order_items(order_id);

-- migrate: 2
-- Delivery address, required by the app when consume is entrega.
ALTER TABLE orders ADD COLUMN address TEXT NOT NULL DEFAULT '';

-- migrate: 3
-- Split bills: an order is paid in one or more parts, each with its own method and amount.
-- Existing payments become one part for the order's total; cash that no longer covered it is dropped.
CREATE TABLE order_payments (
  id               INTEGER PRIMARY KEY,
  order_id         INTEGER NOT NULL REFERENCES orders(id),
  method           TEXT NOT NULL CHECK (method IN ('pix','credito','debito','dinheiro')),
  amount_cents     INTEGER NOT NULL CHECK (amount_cents >= 0),
  cash_given_cents INTEGER CHECK (cash_given_cents >= amount_cents),
  paid_at          TEXT NOT NULL
);

CREATE INDEX order_payments_order ON order_payments(order_id);

INSERT INTO order_payments (order_id, method, amount_cents, cash_given_cents, paid_at)
SELECT id, payment, total, CASE WHEN cash >= total THEN cash END, COALESCE(paid_at, created_at)
FROM (SELECT o.id, o.payment, o.cash_given_cents AS cash, o.paid_at, o.created_at,
        (SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = o.id) AS total
      FROM orders o WHERE o.payment IS NOT NULL);

ALTER TABLE orders DROP COLUMN payment;
ALTER TABLE orders DROP COLUMN paid_at;
ALTER TABLE orders DROP COLUMN cash_given_cents;

-- migrate: 4
-- Opening cash per local day (YYYY-MM-DD, America/Sao_Paulo), for the expected cash in the report.
CREATE TABLE cash_days (
  day           TEXT PRIMARY KEY,
  opening_cents INTEGER NOT NULL CHECK (opening_cents >= 0)
);
