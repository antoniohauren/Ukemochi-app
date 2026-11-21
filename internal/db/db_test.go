package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// seed creates one category, item and product priced 600 and returns the product id.
func seed(t *testing.T, d *DB) int64 {
	t.Helper()
	ctx := context.Background()
	cat, err := d.CreateCategory(ctx, "Refrigerante", 1)
	if err != nil {
		t.Fatal(err)
	}
	item, err := d.CreateItem(ctx, Item{CategoryID: cat, Name: "Coca-Cola", Active: true})
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreateProduct(ctx, Product{ItemID: item, Variant: "Zero", Size: "Lata",
		PrintableName: "COCA ZERO LT", PriceCents: 600, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func newOrder(t *testing.T, d *DB, consume string) int64 {
	t.Helper()
	id, err := d.CreateOrder(context.Background(), NewOrder{Channel: "whatsapp", Consume: consume, ClientName: "Maria", Address: "Rua A, 1"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestMigrateIdempotent(t *testing.T) {
	d := openTest(t)
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var v int
	d.QueryRow("PRAGMA user_version").Scan(&v)
	if v != 4 {
		t.Fatalf("user_version = %d, want 4", v)
	}
	var fk int
	d.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign_keys off")
	}
}

func TestMigrateRefusesNewerDB(t *testing.T) {
	d := openTest(t)
	if _, err := d.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err == nil {
		t.Fatal("want error for a database newer than the app")
	}
}

func TestStepsOrder(t *testing.T) {
	if _, err := steps("-- migrate: 1\nA;\n-- migrate: 3\nB;\n"); err == nil {
		t.Fatal("want error for skipped version")
	}
	s, err := steps("-- migrate: 1\nA;\n-- migrate: 2\nB;\n")
	if err != nil || len(s) != 2 || s[1].sql != "\nB;\n" {
		t.Fatalf("steps = %#v, %v", s, err)
	}
}

func TestAddItemSnapshotAndTotal(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	p := seed(t, d)
	id := newOrder(t, d, ConsumeDelivery)

	if err := d.AddItem(ctx, id, p, 1, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem(ctx, id, p, 2, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem(ctx, id, p, 1, "sem gelo"); err != nil {
		t.Fatal(err)
	}
	// A later price change must not touch the snapshot.
	if err := d.UpdateProduct(ctx, Product{ID: p, Variant: "Zero", Size: "Lata", PrintableName: "X", PriceCents: 900, Active: true}); err != nil {
		t.Fatal(err)
	}

	o, err := d.GetOrder(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Items) != 2 || o.Items[0].Qty != 3 || o.Items[0].NameSnapshot != "Coca-Cola Zero Lata" {
		t.Fatalf("items = %+v", o.Items)
	}
	if o.TotalCents != 2400 {
		t.Fatalf("total = %d, want 2400", o.TotalCents)
	}

	if err := d.SetItemQty(ctx, id, o.Items[1].ID, 0); err != nil {
		t.Fatal(err)
	}
	if o, _ = d.GetOrder(ctx, id); len(o.Items) != 1 || o.TotalCents != 1800 {
		t.Fatalf("after remove: %+v", o)
	}
}

func TestAddItemRefused(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	p := seed(t, d)
	id := newOrder(t, d, ConsumeDelivery)

	for _, q := range []int64{0, -1, MaxQty + 1} {
		if err := d.AddItem(ctx, id, p, q, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("qty %d: %v", q, err)
		}
	}
	if err := d.AddItem(ctx, id, p, MaxQty, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem(ctx, id, p, 1, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("merge past MaxQty: %v", err)
	}
	o, _ := d.GetOrder(ctx, id)
	if err := d.SetItemQty(ctx, id, o.Items[0].ID, MaxQty+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SetItemQty past MaxQty: %v", err)
	}
	other := newOrder(t, d, ConsumeDelivery)
	if err := d.SetItemQty(ctx, other, o.Items[0].ID, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("item of another order: %v", err)
	}
	if err := d.AddPayment(ctx, id, "cheque", 100, 0, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad payment method: %v", err)
	}
	if err := d.AddItem(ctx, id, 999, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown product: %v", err)
	}
	if err := d.UpdateProduct(ctx, Product{ID: p, PrintableName: "X", PriceCents: 600, Active: false}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem(ctx, id, p, 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive product: %v", err)
	}
	d.UpdateProduct(ctx, Product{ID: p, PrintableName: "X", PriceCents: 600, Active: true})
	if err := d.SetStatus(ctx, id, StatusKitchen); err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem(ctx, id, p, 1, ""); !errors.Is(err, ErrNotEditable) {
		t.Fatalf("add to cozinha order: %v", err)
	}
}

func TestStatusTransitions(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	p := seed(t, d)
	// order returns a new order holding one item, so it may go to the kitchen.
	order := func(consume string) int64 {
		id := newOrder(t, d, consume)
		if err := d.AddItem(ctx, id, p, 1, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}

	t.Run("empty order stays out of the kitchen", func(t *testing.T) {
		if err := d.SetStatus(ctx, newOrder(t, d, ConsumeLocal), StatusKitchen); !errors.Is(err, ErrEmptyOrder) {
			t.Fatalf("empty to kitchen: %v", err)
		}
	})

	t.Run("delivery full path", func(t *testing.T) {
		id := order(ConsumeDelivery)
		for _, s := range []string{StatusKitchen, StatusReady} {
			if err := d.SetStatus(ctx, id, s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		if err := d.SetStatus(ctx, id, StatusClosed); !errors.Is(err, ErrBadTransition) {
			t.Fatalf("pronto>fechado for entrega: %v", err)
		}
		if err := d.SetStatus(ctx, id, StatusOut); err != nil {
			t.Fatal(err)
		}
		if err := d.SetStatus(ctx, id, StatusClosed); !errors.Is(err, ErrUnpaid) {
			t.Fatalf("closing unpaid: %v", err)
		}
		if err := d.AddPayment(ctx, id, "dinheiro", 600, 5000, 1); err != nil {
			t.Fatal(err)
		}
		if err := d.SetStatus(ctx, id, StatusClosed); err != nil {
			t.Fatal(err)
		}
		o, _ := d.GetOrder(ctx, id)
		if o.Status != StatusClosed || o.ClosedAt == "" || len(o.Payments) != 1 || o.Payments[0].CashGivenCents != 5000 || o.Payments[0].PaidAt == "" {
			t.Fatalf("closed order = %+v", o)
		}
		if err := d.DeletePayment(ctx, id, o.Payments[0].ID); !errors.Is(err, ErrBadTransition) {
			t.Fatalf("unpay closed order: %v", err)
		}
		if err := d.SetStatus(ctx, id, StatusOpen); err != nil {
			t.Fatalf("reopen closed: %v", err)
		}
		o, _ = d.GetOrder(ctx, id)
		if o.Status != StatusOpen || o.ClosedAt != "" || !o.Paid() || o.Payments[0].Method != "dinheiro" {
			t.Fatalf("reopened order = %+v", o)
		}
		if err := d.DeletePayment(ctx, id, o.Payments[0].ID); err != nil {
			t.Fatalf("unpay reopened order: %v", err)
		}
	})

	t.Run("parts short of the total refuse close", func(t *testing.T) {
		id := order(ConsumeLocal)
		d.SetStatus(ctx, id, StatusKitchen)
		d.SetStatus(ctx, id, StatusReady)
		if err := d.AddPayment(ctx, id, "pix", 100, 0, 1); err != nil {
			t.Fatal(err)
		}
		if err := d.SetStatus(ctx, id, StatusClosed); !errors.Is(err, ErrPaidMismatch) {
			t.Fatalf("close with a part left: %v", err)
		}
	})

	t.Run("saiu refused for local", func(t *testing.T) {
		id := order(ConsumeLocal)
		d.SetStatus(ctx, id, StatusKitchen)
		d.SetStatus(ctx, id, StatusReady)
		if err := d.SetStatus(ctx, id, StatusOut); !errors.Is(err, ErrBadTransition) {
			t.Fatalf("saiu for local: %v", err)
		}
		d.AddPayment(ctx, id, "credito", 600, 999, 1)
		if err := d.SetStatus(ctx, id, StatusClosed); err != nil {
			t.Fatal(err)
		}
		if o, _ := d.GetOrder(ctx, id); o.Payments[0].CashGivenCents != 0 {
			t.Fatalf("cash stored for credito: %d", o.Payments[0].CashGivenCents)
		}
	})

	t.Run("skips and cancel", func(t *testing.T) {
		id := order(ConsumeDelivery)
		if err := d.SetStatus(ctx, id, StatusReady); !errors.Is(err, ErrBadTransition) {
			t.Fatalf("aberto>pronto: %v", err)
		}
		if err := d.SetStatus(ctx, id, "bogus"); !errors.Is(err, ErrBadTransition) {
			t.Fatalf("bogus status: %v", err)
		}
		if err := d.SetStatus(ctx, id, StatusCancelled); err != nil {
			t.Fatal(err)
		}
		if err := d.SetStatus(ctx, id, StatusKitchen); !errors.Is(err, ErrBadTransition) {
			t.Fatalf("cancelado>cozinha: %v", err)
		}
		if err := d.SetStatus(ctx, id, StatusOpen); err != nil {
			t.Fatalf("reopen cancelled: %v", err)
		}
		if err := d.SetStatus(ctx, 999, StatusKitchen); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing order: %v", err)
		}
	})
}

func TestListOpenOrdersAndMenu(t *testing.T) {
	ctx := context.Background()
	d := openTest(t)
	p := seed(t, d)
	a := newOrder(t, d, ConsumeLocal)
	b := newOrder(t, d, ConsumeLocal)
	d.AddItem(ctx, a, p, 2, "")
	d.SetStatus(ctx, b, StatusCancelled)

	open, err := d.ListOpenOrders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != a || open[0].TotalCents != 1200 {
		t.Fatalf("open = %+v", open)
	}

	if err := d.SetConsume(ctx, a, ConsumeDelivery, "Rua B, 2"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetConsume(ctx, a, "x", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid consume accepted")
	}

	menu, err := d.Menu(ctx, true)
	if err != nil || len(menu) != 1 || len(menu[0].Items) != 1 || len(menu[0].Items[0].Products) != 1 {
		t.Fatalf("menu = %+v, %v", menu, err)
	}
	d.UpdateProduct(ctx, Product{ID: p, PrintableName: "X", Active: false})
	if menu, _ = d.Menu(ctx, true); len(menu[0].Items) != 0 {
		t.Fatalf("item with no active product listed: %+v", menu)
	}
	if menu, _ = d.Menu(ctx, false); len(menu[0].Items[0].Products) != 1 {
		t.Fatalf("admin menu hides inactive: %+v", menu)
	}
}

func TestClientLabel(t *testing.T) {
	if got := (Order{ClientName: "Antonio Carlos", PhoneLast4: "8888"}).ClientLabel(); got != "Antonio Carlos - 8888" {
		t.Errorf("with phone: %q", got)
	}
	if got := (Order{ClientName: "Ana"}).ClientLabel(); got != "Ana" {
		t.Errorf("without phone: %q", got)
	}
}
