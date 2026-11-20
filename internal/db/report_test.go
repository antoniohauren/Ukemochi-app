package db

import (
	"context"
	"testing"
	"time"
)

// closedOrder creates an eat-in order with qty of product p, pays it with method and closes it.
func closedOrder(t *testing.T, d *DB, p, qty int64, channel, method string) int64 {
	t.Helper()
	ctx := context.Background()
	id, err := d.CreateOrder(ctx, NewOrder{Channel: channel, Consume: ConsumeLocal, ClientName: "Maria"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.AddItem(ctx, id, p, qty, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPayment(ctx, id, method, 0); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{StatusKitchen, StatusReady, StatusClosed} {
		if err := d.SetStatus(ctx, id, s); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestDailyReport(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	p := seed(t, d) // R$ 6,00

	closedOrder(t, d, p, 2, "whatsapp", "pix")
	closedOrder(t, d, p, 1, "balcao", "pix")
	closedOrder(t, d, p, 3, "balcao", "dinheiro")
	reopened := closedOrder(t, d, p, 5, "balcao", "credito")
	if err := d.SetStatus(ctx, reopened, StatusOpen); err != nil {
		t.Fatal(err)
	}
	yesterday := closedOrder(t, d, p, 7, "balcao", "debito")
	if _, err := d.Exec(`UPDATE orders SET closed_at = ? WHERE id = ?`,
		time.Now().Add(-24*time.Hour).UTC().Format(timeFormat), yesterday); err != nil {
		t.Fatal(err)
	}
	cancelled := newOrder(t, d, ConsumeLocal)
	if err := d.AddItem(ctx, cancelled, p, 9, ""); err != nil {
		t.Fatal(err)
	}
	if err := d.SetPayment(ctx, cancelled, "pix", 0); err != nil {
		t.Fatal(err)
	}
	if err := d.SetStatus(ctx, cancelled, StatusCancelled); err != nil {
		t.Fatal(err)
	}
	unpaid := newOrder(t, d, ConsumeLocal)
	if err := d.AddItem(ctx, unpaid, p, 4, ""); err != nil {
		t.Fatal(err)
	}

	from := time.Now().Add(-time.Hour)
	rep, err := d.DailyReport(ctx, from, from.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Orders != 3 || rep.TotalCents != 3600 {
		t.Fatalf("totals = %d orders, %d cents; want 3, 3600", rep.Orders, rep.TotalCents)
	}
	wantPay := []ReportRow{{"dinheiro", 1, 1800}, {"pix", 2, 1800}}
	if len(rep.ByPayment) != 2 || rep.ByPayment[0] != wantPay[0] || rep.ByPayment[1] != wantPay[1] {
		t.Errorf("ByPayment = %+v, want %+v", rep.ByPayment, wantPay)
	}
	wantCh := []ReportRow{{"balcao", 2, 2400}, {"whatsapp", 1, 1200}}
	if len(rep.ByChannel) != 2 || rep.ByChannel[0] != wantCh[0] || rep.ByChannel[1] != wantCh[1] {
		t.Errorf("ByChannel = %+v, want %+v", rep.ByChannel, wantCh)
	}
	if len(rep.TopItems) != 1 || rep.TopItems[0] != (TopItem{"Coca-Cola Zero Lata", 6, 3600}) {
		t.Errorf("TopItems = %+v", rep.TopItems)
	}

	// The range is half-open: an order closed exactly at `to` belongs to the next day.
	var closedAt string
	if err := d.QueryRow(`SELECT MAX(closed_at) FROM orders WHERE status = 'fechado'`).Scan(&closedAt); err != nil {
		t.Fatal(err)
	}
	at, _ := time.Parse(timeFormat, closedAt)
	if rep, _ := d.DailyReport(ctx, at.Add(-time.Hour), at); rep.Orders != 0 {
		t.Errorf("order closed at the upper bound counted: %+v", rep)
	}
	if rep, _ := d.DailyReport(ctx, at, at.Add(time.Second)); rep.Orders == 0 {
		t.Error("order closed at the lower bound not counted")
	}

	empty, err := d.DailyReport(ctx, from.Add(-72*time.Hour), from.Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Orders != 0 || len(empty.ByPayment) != 0 || len(empty.TopItems) != 0 {
		t.Errorf("empty day = %+v", empty)
	}
}
