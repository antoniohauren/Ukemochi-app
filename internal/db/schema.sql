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
