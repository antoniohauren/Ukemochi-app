package db

import (
	"context"
	"database/sql"
	"time"
)

// ReportRow is one group of closed orders: a payment method or a channel.
type ReportRow struct {
	Key        string
	Orders     int64
	TotalCents int64
}

// TopItem is a product name with the quantity sold and its revenue.
type TopItem struct {
	Name       string
	Qty        int64
	TotalCents int64
}

// Report sums the orders closed in [from, to).
type Report struct {
	Orders     int64
	TotalCents int64
	ByPayment  []ReportRow
	ByChannel  []ReportRow
	TopItems   []TopItem
}

// topItemsLimit caps the "most sold" list.
const topItemsLimit = 10

// DailyReport sums the fechado orders whose closed_at is in [from, to).
// Reopened orders have no closed_at, so they drop out until closed again.
func (d *DB) DailyReport(ctx context.Context, from, to time.Time) (Report, error) {
	var rep Report
	lo, hi := from.UTC().Format(timeFormat), to.UTC().Format(timeFormat)
	scanRow := func(r *sql.Rows, row *ReportRow) error { return r.Scan(&row.Key, &row.Orders, &row.TotalCents) }
	var err error
	// A split bill counts once under each method it used, with that part's amount.
	// Closed orders are paid exactly, so the methods add up to the day's total.
	rep.ByPayment, err = queryAll(ctx, d, `SELECT op.method, COUNT(DISTINCT o.id), SUM(op.amount_cents)
		FROM orders o JOIN order_payments op ON op.order_id = o.id
		WHERE o.status = 'fechado' AND o.closed_at >= ? AND o.closed_at < ?
		GROUP BY 1 ORDER BY 3 DESC, 1`, scanRow, lo, hi)
	if err != nil {
		return rep, err
	}
	rep.ByChannel, err = queryAll(ctx, d, `SELECT o.channel, COUNT(DISTINCT o.id), SUM(oi.qty * oi.unit_price_cents)
		FROM orders o JOIN order_items oi ON oi.order_id = o.id
		WHERE o.status = 'fechado' AND o.closed_at >= ? AND o.closed_at < ?
		GROUP BY 1 ORDER BY 3 DESC, 1`, scanRow, lo, hi)
	if err != nil {
		return rep, err
	}
	for _, row := range rep.ByChannel {
		rep.Orders += row.Orders
		rep.TotalCents += row.TotalCents
	}
	rep.TopItems, err = queryAll(ctx, d, `SELECT oi.name_snapshot, SUM(oi.qty), SUM(oi.qty * oi.unit_price_cents)
		FROM orders o JOIN order_items oi ON oi.order_id = o.id
		WHERE o.status = 'fechado' AND o.closed_at >= ? AND o.closed_at < ?
		GROUP BY oi.name_snapshot ORDER BY 2 DESC, 3 DESC, 1 LIMIT ?`,
		func(r *sql.Rows, it *TopItem) error { return r.Scan(&it.Name, &it.Qty, &it.TotalCents) }, lo, hi, topItemsLimit)
	return rep, err
}
