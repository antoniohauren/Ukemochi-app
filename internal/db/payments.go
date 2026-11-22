package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
)

// Payment is one part of an order's bill: the whole total, a fixed amount, an equal share or what one
// person consumed.
type Payment struct {
	ID             int64
	OrderID        int64
	Method         string
	AmountCents    int64
	CashGivenCents int64 // 0 when not paid in cash or not given
	PaidAt         string
	Items          []PaymentItem // the units this part covers when paid by consumption; filled by GetOrder
}

// PaymentItem is qty units of one order line covered by a payment part.
type PaymentItem struct {
	PaymentID   int64
	OrderItemID int64
	Qty         int64  // parts of the line: units, or 1/SplitParts of a unit when the line is split
	SplitParts  int64  // the line's split_parts
	Name        string // the line's name_snapshot
}

func (d *DB) payments(ctx context.Context, orderID int64) ([]Payment, error) {
	ps, err := queryAll(ctx, d, `SELECT id, order_id, method, amount_cents, COALESCE(cash_given_cents, 0), paid_at
		FROM order_payments WHERE order_id = ? ORDER BY id`,
		func(r *sql.Rows, p *Payment) error {
			return r.Scan(&p.ID, &p.OrderID, &p.Method, &p.AmountCents, &p.CashGivenCents, &p.PaidAt)
		}, orderID)
	if err != nil || len(ps) == 0 {
		return ps, err
	}
	items, err := queryAll(ctx, d, `SELECT opi.payment_id, opi.order_item_id, opi.qty, oi.split_parts, oi.name_snapshot
		FROM order_payment_items opi
		JOIN order_payments op ON op.id = opi.payment_id
		JOIN order_items oi ON oi.id = opi.order_item_id
		WHERE op.order_id = ? ORDER BY oi.id`,
		func(r *sql.Rows, it *PaymentItem) error {
			return r.Scan(&it.PaymentID, &it.OrderItemID, &it.Qty, &it.SplitParts, &it.Name)
		}, orderID)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if i := slices.IndexFunc(ps, func(p Payment) bool { return p.ID == it.PaymentID }); i >= 0 {
			ps[i].Items = append(ps[i].Items, it)
		}
	}
	return ps, nil
}

// AddPayment records one payment part, at any status except fechado and cancelado.
// The amount may not exceed what is left to pay, and is above 0 unless the order costs nothing.
// cashGivenCents is stored only for dinheiro, only when above 0, and must cover the amount.
// people is how many split the bill; it is stored on the order so the next part starts from it.
func (d *DB) AddPayment(ctx context.Context, orderID int64, method string, amountCents, cashGivenCents, people int64) error {
	if !slices.Contains(PaymentMethods, method) || amountCents < 0 || people < 1 {
		return ErrInvalid
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := insertPayment(ctx, tx, orderID, method, amountCents, cashGivenCents); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE orders SET split_people = ? WHERE id = ?`, people, orderID)
		return err
	})
}

// AddConsumptionPayment records one payment part for what one person consumed: units maps an order line
// to how many of its units this part covers. The amount is the sum of those units at the line's price.
// Each line must belong to the order and still have that many units unpaid; the amount follows AddPayment's rules.
// It returns the amount recorded.
func (d *DB) AddConsumptionPayment(ctx context.Context, orderID int64, method string, cashGivenCents int64, units map[int64]int64) (int64, error) {
	if !slices.Contains(PaymentMethods, method) || len(units) == 0 {
		return 0, ErrInvalid
	}
	var amount int64
	err := d.tx(ctx, func(tx *sql.Tx) error {
		amount = 0
		for itemID, n := range units {
			var it OrderItem
			err := tx.QueryRowContext(ctx, `SELECT qty, unit_price_cents, split_parts,
				(SELECT COALESCE(SUM(qty), 0) FROM order_payment_items WHERE order_item_id = order_items.id)
				FROM order_items WHERE id = ? AND order_id = ?`, itemID, orderID).
				Scan(&it.Qty, &it.UnitPriceCents, &it.SplitParts, &it.PaidQty)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if n < 1 || n > it.UnpaidQty() {
				return ErrInvalid
			}
			amount += it.PartsCost(it.PaidQty, n)
		}
		pid, err := insertPayment(ctx, tx, orderID, method, amount, cashGivenCents)
		if err != nil {
			return err
		}
		for itemID, n := range units {
			if _, err := tx.ExecContext(ctx, `INSERT INTO order_payment_items (payment_id, order_item_id, qty)
				VALUES (?, ?, ?)`, pid, itemID, n); err != nil {
				return err
			}
		}
		return nil
	})
	return amount, err
}

// insertPayment adds one payment part inside tx after checking the order's status and what is left to pay.
func insertPayment(ctx context.Context, tx *sql.Tx, orderID int64, method string, amountCents, cashGivenCents int64) (int64, error) {
	var cash any
	if method == "dinheiro" && cashGivenCents > 0 {
		if cashGivenCents < amountCents {
			return 0, ErrCashShort
		}
		cash = cashGivenCents
	}
	var status string
	var total, paid, parts int64
	err := tx.QueryRowContext(ctx, `SELECT status,
		(SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = orders.id),
		(SELECT COALESCE(SUM(amount_cents), 0) FROM order_payments WHERE order_id = orders.id),
		(SELECT COUNT(*) FROM order_payments WHERE order_id = orders.id)
		FROM orders WHERE id = ?`, orderID).Scan(&status, &total, &paid, &parts)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if status == StatusClosed || status == StatusCancelled {
		return 0, ErrBadTransition
	}
	if amountCents > total-paid {
		return 0, ErrOverpaid
	}
	// A zero part only settles a free order that has no payment yet.
	if amountCents == 0 && (total != 0 || parts > 0) {
		return 0, ErrInvalid
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO order_payments (order_id, method, amount_cents, cash_given_cents, paid_at)
		VALUES (?, ?, ?, ?, ?)`, orderID, method, amountCents, cash, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeletePayment removes one payment part, at any status except fechado and cancelado.
// Units it covered by consumption become unpaid again.
func (d *DB) DeletePayment(ctx context.Context, orderID, paymentID int64) error {
	return d.tx(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, orderID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == StatusClosed || status == StatusCancelled {
			return ErrBadTransition
		}
		return execOne(ctx, tx, `DELETE FROM order_payments WHERE id = ? AND order_id = ?`, paymentID, orderID)
	})
}

// EqualShare is one person's share when total is split among n people, rounded up to the centavo.
// The last person pays what is left, so the parts always add up to the total.
func EqualShare(total, n int64) int64 {
	if n < 1 {
		return total
	}
	return (total + n - 1) / n
}

// SetExpectedPayment records how a delivery client will pay what is left, at any status except fechado and
// cancelado. An empty method clears it. changeForCents is kept only for dinheiro, and only when above 0.
// An order eaten in pays at the counter, so it has no expected payment.
func (d *DB) SetExpectedPayment(ctx context.Context, orderID int64, method string, changeForCents int64) error {
	if method != "" && !slices.Contains(PaymentMethods, method) || changeForCents < 0 {
		return ErrInvalid
	}
	var m, change any
	if method != "" {
		m = method
	}
	if method == "dinheiro" && changeForCents > 0 {
		change = changeForCents
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		var status, consume string
		err := tx.QueryRowContext(ctx, `SELECT status, consume FROM orders WHERE id = ?`, orderID).Scan(&status, &consume)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == StatusClosed || status == StatusCancelled {
			return ErrBadTransition
		}
		if consume != ConsumeDelivery && m != nil {
			return ErrInvalid
		}
		return execOne(ctx, tx, `UPDATE orders SET expected_method = ?, change_for_cents = ? WHERE id = ?`, m, change, orderID)
	})
}
