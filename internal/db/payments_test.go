package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestSplitPayment(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	p := seed(t, d) // R$ 6,00
	id := newOrder(t, d, ConsumeLocal)
	if err := d.AddItem(ctx, id, p, 5, ""); err != nil { // R$ 30,00
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name         string
		method       string
		amount, cash int64
		want         error
	}{
		{"bad method", "cheque", 1000, 0, ErrInvalid},
		{"negative", "pix", -1, 0, ErrInvalid},
		{"zero", "pix", 0, 0, ErrInvalid},
		{"over the total", "pix", 3001, 0, ErrOverpaid},
		{"cash short of the part", "dinheiro", 1000, 999, ErrCashShort},
	} {
		if err := d.AddPayment(ctx, id, tc.method, tc.amount, tc.cash); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := d.AddPayment(ctx, 999, "pix", 100, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing order: %v", err)
	}

	// Three people split R$ 30,00: R$ 10,00 by PIX, R$ 10,00 in cash (R$ 20,00 given), the rest by card.
	if err := d.AddPayment(ctx, id, "pix", EqualShare(3000, 3), 0); err != nil {
		t.Fatal(err)
	}
	if err := d.AddPayment(ctx, id, "dinheiro", 1000, 2000); err != nil {
		t.Fatal(err)
	}
	o, _ := d.GetOrder(ctx, id)
	if o.PaidCents != 2000 || o.PaymentCount != 2 || o.RemainingCents() != 1000 || o.Paid() {
		t.Fatalf("after two parts: paid %d, count %d", o.PaidCents, o.PaymentCount)
	}
	if err := d.AddPayment(ctx, id, "credito", 1001, 0); !errors.Is(err, ErrOverpaid) {
		t.Fatalf("over what is left: %v", err)
	}
	if err := d.AddPayment(ctx, id, "credito", 1000, 0); err != nil {
		t.Fatal(err)
	}
	if o, _ = d.GetOrder(ctx, id); !o.Paid() || len(o.Payments) != 3 || o.Payments[1].CashGivenCents != 2000 {
		t.Fatalf("paid order = %+v", o)
	}
	if err := d.AddPayment(ctx, id, "pix", 1, 0); !errors.Is(err, ErrOverpaid) {
		t.Fatalf("pay a paid order: %v", err)
	}

	// Removing a part reopens the gap; a part of another order is not found.
	other := newOrder(t, d, ConsumeLocal)
	if err := d.DeletePayment(ctx, other, o.Payments[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("part of another order: %v", err)
	}
	if err := d.DeletePayment(ctx, id, o.Payments[0].ID); err != nil {
		t.Fatal(err)
	}
	if o, _ = d.GetOrder(ctx, id); o.RemainingCents() != 1000 || o.Paid() {
		t.Fatalf("after removing a part: remaining %d", o.RemainingCents())
	}

	// A free order is settled by one zero part.
	free := newOrder(t, d, ConsumeLocal)
	if err := d.AddPayment(ctx, free, "pix", 0, 0); err != nil {
		t.Fatalf("free order: %v", err)
	}
	if err := d.AddPayment(ctx, free, "pix", 0, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second zero part: %v", err)
	}
}

func TestEqualShare(t *testing.T) {
	for _, tc := range []struct{ total, n, want int64 }{
		{3000, 3, 1000},
		{10000, 3, 3334},
		{1, 2, 1},
		{500, 0, 500},
	} {
		if got := EqualShare(tc.total, tc.n); got != tc.want {
			t.Errorf("EqualShare(%d, %d) = %d, want %d", tc.total, tc.n, got, tc.want)
		}
	}
}

// TestMigratePayments checks that migration 3 turns each recorded payment into one part for the order's total.
func TestMigratePayments(t *testing.T) {
	ctx := context.Background()
	// Open without migrating, then build a version 2 database by hand.
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	d := &DB{raw}
	t.Cleanup(func() { d.Close() })
	all, err := steps(schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all[:2] {
		if _, err := d.Exec(s.sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO categories (id, name) VALUES (1, 'C')`,
		`INSERT INTO items (id, category_id, name) VALUES (1, 1, 'I')`,
		`INSERT INTO products (id, item_id, printable_name, price_cents) VALUES (1, 1, 'P', 1500)`,
		`INSERT INTO orders (id, channel, consume, client_name, status, payment, paid_at, cash_given_cents, created_at)
			VALUES (1, 'balcao', 'local', 'A', 'fechado', 'dinheiro', '2026-01-01T10:00:00Z', 5000, '2026-01-01T09:00:00Z'),
			       (2, 'balcao', 'local', 'B', 'aberto', 'dinheiro', '2026-01-01T10:00:00Z', 1000, '2026-01-01T09:00:00Z'),
			       (3, 'balcao', 'local', 'C', 'aberto', NULL, NULL, NULL, '2026-01-01T09:00:00Z')`,
		`INSERT INTO order_items (order_id, product_id, qty, unit_price_cents, name_snapshot) VALUES (1, 1, 2, 1500, 'I'), (2, 1, 2, 1500, 'I'), (3, 1, 1, 1500, 'I')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	o1, _ := d.GetOrder(ctx, 1)
	if !o1.Paid() || o1.Payments[0].Method != "dinheiro" || o1.Payments[0].AmountCents != 3000 || o1.Payments[0].CashGivenCents != 5000 {
		t.Errorf("order 1 = %+v", o1.Payments)
	}
	// Cash that no longer covered the total is dropped; the part still pays the total.
	if o2, _ := d.GetOrder(ctx, 2); !o2.Paid() || o2.Payments[0].CashGivenCents != 0 {
		t.Errorf("order 2 = %+v", o2.Payments)
	}
	if o3, _ := d.GetOrder(ctx, 3); o3.PaymentCount != 0 {
		t.Errorf("order 3 = %+v", o3.Payments)
	}
}
