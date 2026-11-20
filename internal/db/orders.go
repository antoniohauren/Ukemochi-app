package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"
)

var (
	ErrBadTransition = errors.New("mudança de status não permitida")
	ErrUnpaid        = errors.New("pedido sem pagamento não pode ser fechado")
	ErrCashShort     = errors.New("dinheiro recebido não cobre o total")
	ErrNotEditable   = errors.New("itens só podem ser alterados com o pedido aberto")
	ErrInvalid       = errors.New("valor inválido")
	ErrEmptyOrder    = errors.New("pedido sem itens não pode ir para a cozinha")
)

// Order statuses, channels, consume modes and payment methods match the CHECK constraints in schema.sql.
const (
	StatusOpen      = "aberto"
	StatusKitchen   = "cozinha"
	StatusReady     = "pronto"
	StatusOut       = "saiu"
	StatusClosed    = "fechado"
	StatusCancelled = "cancelado"

	ConsumeDelivery = "entrega"
	ConsumeLocal    = "local"

	MaxQty = 999 // matches the CHECK on order_items.qty
)

// PaymentMethods lists the values allowed in orders.payment.
var PaymentMethods = []string{"pix", "credito", "debito", "dinheiro"}

// timeFormat is fixed-width UTC so stored timestamps compare correctly as text.
const timeFormat = "2006-01-02T15:04:05Z"

func now() string { return time.Now().UTC().Format(timeFormat) }

type Order struct {
	ID             int64
	Channel        string
	Consume        string
	ClientName     string
	PhoneLast4     string
	Status         string
	Payment        string // empty while unpaid
	PaidAt         string
	CashGivenCents int64 // 0 when not paid in cash or not given
	Notes          string
	CreatedAt      string
	ClosedAt       string
	TotalCents     int64
	Items          []OrderItem // filled by GetOrder only
}

type OrderItem struct {
	ID             int64
	OrderID        int64
	ProductID      int64
	Qty            int64
	UnitPriceCents int64
	NameSnapshot   string
	Note           string
	PrintableName  string // current products.printable_name, for the ticket; filled by GetOrder
}

// CanTransition reports whether an order may move from status from to status to.
// It does not check payment; SetStatus does that for fechado.
func CanTransition(from, to, consume string) bool {
	switch from + ">" + to {
	case "aberto>cozinha", "cozinha>pronto", "saiu>fechado",
		"aberto>cancelado", "cozinha>cancelado", "pronto>cancelado",
		"fechado>aberto", "cancelado>aberto": // reopen at any time

		return true
	case "pronto>saiu":
		return consume == ConsumeDelivery
	case "pronto>fechado":
		return consume == ConsumeLocal
	}
	return false
}

// NewOrder holds the fields set when an order is created.
type NewOrder struct {
	Channel    string
	Consume    string
	ClientName string
	PhoneLast4 string
	Notes      string
}

func (d *DB) CreateOrder(ctx context.Context, o NewOrder) (int64, error) {
	return insert(ctx, d, `INSERT INTO orders (channel, consume, client_name, phone_last4, notes, created_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?)`, o.Channel, o.Consume, o.ClientName, o.PhoneLast4, o.Notes, now())
}

const orderCols = `o.id, o.channel, o.consume, o.client_name, COALESCE(o.phone_last4, ''), o.status,
	COALESCE(o.payment, ''), COALESCE(o.paid_at, ''), COALESCE(o.cash_given_cents, 0), o.notes,
	o.created_at, COALESCE(o.closed_at, ''),
	(SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = o.id)`

func scanOrder(r interface{ Scan(...any) error }, o *Order) error {
	return r.Scan(&o.ID, &o.Channel, &o.Consume, &o.ClientName, &o.PhoneLast4, &o.Status,
		&o.Payment, &o.PaidAt, &o.CashGivenCents, &o.Notes, &o.CreatedAt, &o.ClosedAt, &o.TotalCents)
}

// GetOrder returns one order with its items and total.
func (d *DB) GetOrder(ctx context.Context, id int64) (Order, error) {
	var o Order
	err := scanOrder(d.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders o WHERE o.id = ?`, id), &o)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	// Products are never deleted, so the join always matches.
	o.Items, err = queryAll(ctx, d, `SELECT oi.id, oi.order_id, oi.product_id, oi.qty, oi.unit_price_cents,
		oi.name_snapshot, oi.note, p.printable_name
		FROM order_items oi JOIN products p ON p.id = oi.product_id WHERE oi.order_id = ? ORDER BY oi.id`,
		func(r *sql.Rows, it *OrderItem) error {
			return r.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.Qty, &it.UnitPriceCents, &it.NameSnapshot, &it.Note,
				&it.PrintableName)
		}, id)
	return o, err
}

// ListOpenOrders returns orders that are neither fechado nor cancelado, newest first, without items.
func (d *DB) ListOpenOrders(ctx context.Context) ([]Order, error) {
	return queryAll(ctx, d, `SELECT `+orderCols+` FROM orders o
		WHERE o.status NOT IN ('fechado', 'cancelado') ORDER BY o.created_at DESC, o.id DESC`,
		func(r *sql.Rows, o *Order) error { return scanOrder(r, o) })
}

// AddItem adds qty of a product to an open order, snapshotting its name and price.
// The same product with the same note is merged into one line.
func (d *DB) AddItem(ctx context.Context, orderID, productID, qty int64, note string) error {
	if qty < 1 || qty > MaxQty {
		return ErrInvalid
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		if err := requireOpen(ctx, tx, orderID); err != nil {
			return err
		}
		var item, variant, size string
		var price int64
		err := tx.QueryRowContext(ctx, `SELECT i.name, p.variant, p.size, p.price_cents
			FROM products p JOIN items i ON i.id = p.item_id
			WHERE p.id = ? AND p.active = 1 AND i.active = 1`, productID).
			Scan(&item, &variant, &size, &price)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		name := DisplayName(item, variant, size)

		var lineID, lineQty int64
		err = tx.QueryRowContext(ctx, `SELECT id, qty FROM order_items
			WHERE order_id = ? AND product_id = ? AND note = ? AND unit_price_cents = ?`,
			orderID, productID, note, price).Scan(&lineID, &lineQty)
		if err == nil {
			if lineQty+qty > MaxQty {
				return ErrInvalid
			}
			_, err = tx.ExecContext(ctx, `UPDATE order_items SET qty = ? WHERE id = ?`, lineQty+qty, lineID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO order_items
			(order_id, product_id, qty, unit_price_cents, name_snapshot, note) VALUES (?, ?, ?, ?, ?, ?)`,
			orderID, productID, qty, price, name, note)
		return err
	})
}

// SetItemQty changes a line's quantity on an open order. A qty of 0 or less removes the line.
func (d *DB) SetItemQty(ctx context.Context, orderID, itemID, qty int64) error {
	if qty > MaxQty {
		return ErrInvalid
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		if err := requireOpen(ctx, tx, orderID); err != nil {
			return err
		}
		if qty <= 0 {
			return execOne(ctx, tx, `DELETE FROM order_items WHERE id = ? AND order_id = ?`, itemID, orderID)
		}
		return execOne(ctx, tx, `UPDATE order_items SET qty = ? WHERE id = ? AND order_id = ?`, qty, itemID, orderID)
	})
}

// SetStatus moves an order to a new status after checking the state machine in docs/flows.md.
// Closing needs a recorded payment and sending to the kitchen needs at least one item.
func (d *DB) SetStatus(ctx context.Context, id int64, to string) error {
	return d.tx(ctx, func(tx *sql.Tx) error {
		var from, consume, payment string
		var cash, total int64
		err := tx.QueryRowContext(ctx, `SELECT status, consume, COALESCE(payment, ''), COALESCE(cash_given_cents, 0),
			(SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = orders.id)
			FROM orders WHERE id = ?`, id).Scan(&from, &consume, &payment, &cash, &total)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !CanTransition(from, to, consume) {
			return ErrBadTransition
		}
		if to == StatusKitchen {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_items WHERE order_id = ?`, id).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return ErrEmptyOrder
			}
		}
		if to == StatusClosed {
			if payment == "" {
				return ErrUnpaid
			}
			if cash > 0 && cash < total {
				return ErrCashShort
			}
			return execOne(ctx, tx, `UPDATE orders SET status = ?, closed_at = ? WHERE id = ?`, to, now(), id)
		}
		// Reopening clears closed_at so the order leaves the daily report until it is closed again.
		return execOne(ctx, tx, `UPDATE orders SET status = ?, closed_at = NULL WHERE id = ?`, to, id)
	})
}

// SetConsume switches between entrega and local while the order is not yet out or finished.
func (d *DB) SetConsume(ctx context.Context, id int64, consume string) error {
	if consume != ConsumeDelivery && consume != ConsumeLocal {
		return ErrInvalid
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		var status string
		err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, id).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != StatusOpen && status != StatusKitchen && status != StatusReady {
			return ErrBadTransition
		}
		return execOne(ctx, tx, `UPDATE orders SET consume = ? WHERE id = ?`, consume, id)
	})
}

// SetPayment records the payment method, at any status except fechado and cancelado.
// cashGivenCents is stored only for dinheiro, and only when above 0.
func (d *DB) SetPayment(ctx context.Context, id int64, method string, cashGivenCents int64) error {
	if !slices.Contains(PaymentMethods, method) {
		return ErrInvalid
	}
	var cash any
	if method == "dinheiro" && cashGivenCents > 0 {
		cash = cashGivenCents
	}
	res, err := d.ExecContext(ctx, `UPDATE orders SET payment = ?, cash_given_cents = ?, paid_at = ?
		WHERE id = ? AND status NOT IN ('fechado', 'cancelado')`, method, cash, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := d.GetOrder(ctx, id); err != nil {
			return err
		}
		return ErrBadTransition
	}
	return nil
}

func requireOpen(ctx context.Context, tx *sql.Tx, orderID int64) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, orderID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != StatusOpen {
		return ErrNotEditable
	}
	return nil
}

// tx runs fn in a transaction, committing on nil and rolling back otherwise.
func (d *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
