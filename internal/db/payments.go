package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
)

// Payment is one part of an order's bill: the whole total, a fixed amount or an equal share.
type Payment struct {
	ID             int64
	OrderID        int64
	Method         string
	AmountCents    int64
	CashGivenCents int64 // 0 when not paid in cash or not given
	PaidAt         string
}

func (d *DB) payments(ctx context.Context, orderID int64) ([]Payment, error) {
	return queryAll(ctx, d, `SELECT id, order_id, method, amount_cents, COALESCE(cash_given_cents, 0), paid_at
		FROM order_payments WHERE order_id = ? ORDER BY id`,
		func(r *sql.Rows, p *Payment) error {
			return r.Scan(&p.ID, &p.OrderID, &p.Method, &p.AmountCents, &p.CashGivenCents, &p.PaidAt)
		}, orderID)
}

// AddPayment records one payment part, at any status except fechado and cancelado.
// The amount may not exceed what is left to pay, and is above 0 unless the order costs nothing.
// cashGivenCents is stored only for dinheiro, only when above 0, and must cover the amount.
// people is how many split the bill; it is stored on the order so the next part starts from it.
func (d *DB) AddPayment(ctx context.Context, orderID int64, method string, amountCents, cashGivenCents, people int64) error {
	if !slices.Contains(PaymentMethods, method) || amountCents < 0 || people < 1 {
		return ErrInvalid
	}
	var cash any
	if method == "dinheiro" && cashGivenCents > 0 {
		if cashGivenCents < amountCents {
			return ErrCashShort
		}
		cash = cashGivenCents
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		var status string
		var total, paid, parts int64
		err := tx.QueryRowContext(ctx, `SELECT status,
			(SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = orders.id),
			(SELECT COALESCE(SUM(amount_cents), 0) FROM order_payments WHERE order_id = orders.id),
			(SELECT COUNT(*) FROM order_payments WHERE order_id = orders.id)
			FROM orders WHERE id = ?`, orderID).Scan(&status, &total, &paid, &parts)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == StatusClosed || status == StatusCancelled {
			return ErrBadTransition
		}
		if amountCents > total-paid {
			return ErrOverpaid
		}
		// A zero part only settles a free order that has no payment yet.
		if amountCents == 0 && (total != 0 || parts > 0) {
			return ErrInvalid
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO order_payments (order_id, method, amount_cents, cash_given_cents, paid_at)
			VALUES (?, ?, ?, ?, ?)`, orderID, method, amountCents, cash, now())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE orders SET split_people = ? WHERE id = ?`, people, orderID)
		return err
	})
}

// DeletePayment removes one payment part, at any status except fechado and cancelado.
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

// SetExpectedPayment records how the client will pay what is left, at any status except fechado and
// cancelado. An empty method clears it. changeForCents is kept only for dinheiro, and only when above 0.
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
		return execOne(ctx, tx, `UPDATE orders SET expected_method = ?, change_for_cents = ? WHERE id = ?`, m, change, orderID)
	})
}
