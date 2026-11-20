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
	group := func(col string) ([]ReportRow, error) {
		// col is one of two fixed column names, never input.
		return queryAll(ctx, d, `SELECT COALESCE(o.`+col+`, ''), COUNT(DISTINCT o.id), SUM(oi.qty * oi.unit_price_cents)
			FROM orders o JOIN order_items oi ON oi.order_id = o.id
			WHERE o.status = 'fechado' AND o.closed_at >= ? AND o.closed_at < ?
			GROUP BY 1 ORDER BY 3 DESC, 1`,
			func(r *sql.Rows, row *ReportRow) error { return r.Scan(&row.Key, &row.Orders, &row.TotalCents) }, lo, hi)
	}
	var err error
	if rep.ByPayment, err = group("payment"); err != nil {
		return rep, err
	}
	if rep.ByChannel, err = group("channel"); err != nil {
		return rep, err
	}
	for _, row := range rep.ByPayment {
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
