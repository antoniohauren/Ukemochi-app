package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"
)

var (
	ErrBadTransition = errors.New("mudança de status não permitida")
	ErrUnpaid        = errors.New("pedido sem pagamento não pode ser fechado")
	ErrPaidMismatch  = errors.New("pagamentos não batem com o total")
	ErrOverpaid      = errors.New("valor maior que o que falta pagar")
	ErrCashShort     = errors.New("dinheiro recebido não cobre o valor")
	ErrNotEditable   = errors.New("itens não podem ser alterados depois que o pedido saiu ou foi fechado")
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
	ConsumePickup   = "retirada" // the client fetches it at the counter

	MaxQty = 999 // matches the CHECK on order_items.qty
)

// PaymentMethods lists the values allowed in order_payments.method.
var PaymentMethods = []string{"pix", "credito", "debito", "dinheiro"}

// timeFormat is fixed-width UTC so stored timestamps compare correctly as text.
const timeFormat = "2006-01-02T15:04:05Z"

func now() string { return time.Now().UTC().Format(timeFormat) }

type Order struct {
	ID           int64
	Channel      string
	Consume      string
	ClientName   string
	PhoneLast4   string
	Status       string
	Notes        string
	Address      string // delivery address; empty for local
	CreatedAt    string
	ClosedAt     string
	TotalCents   int64
	PaidCents    int64 // sum of the payment parts
	PaymentCount int64 // number of payment parts
	SplitPeople  int64 // how many people split the bill, kept from the last payment part
	// ExpectedMethod is how the client will pay what is left, empty when unknown. ChangeForCents is the
	// note they pay with, set only for dinheiro; 0 when not given.
	ExpectedMethod string
	ChangeForCents int64
	Items          []OrderItem // filled by GetOrder only
	Payments       []Payment   // filled by GetOrder only, oldest first
}

// RemainingCents is what is still to pay; negative when the parts add up to more than the total.
func (o Order) RemainingCents() int64 { return o.TotalCents - o.PaidCents }

// Paid reports whether the payment parts cover the total exactly, so the order may close.
func (o Order) Paid() bool { return o.PaymentCount > 0 && o.PaidCents == o.TotalCents }

// ClientLabel is the client name with the phone's last 4 digits when known, e.g. "Antonio Carlos - 8888".
func (o Order) ClientLabel() string {
	if o.PhoneLast4 == "" {
		return o.ClientName
	}
	return o.ClientName + " - " + o.PhoneLast4
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
	SplitParts     int64  // how many parts each unit is split into for paying by consumption; 1 when not shared
	PaidQty        int64  // parts covered by payment parts made by consumption; filled by GetOrder
}

// Parts is how many parts the line can be paid in by consumption: Qty when not split, Qty * SplitParts otherwise.
func (it OrderItem) Parts() int64 { return it.Qty * max(it.SplitParts, 1) }

// UnpaidQty is how many parts of the line are still free to pay by consumption.
func (it OrderItem) UnpaidQty() int64 { return max(0, it.Parts()-it.PaidQty) }

// PartsCost is what n parts cost after the first from are paid. The first m parts cost the line total * m / Parts,
// rounded up, so every part costs the same give or take a centavo and all of them add up to the line total.
func (it OrderItem) PartsCost(from, n int64) int64 {
	return partsCost(it.Qty*it.UnitPriceCents, it.Parts(), from, n)
}

func partsCost(lineTotal, parts, from, n int64) int64 {
	cum := func(m int64) int64 {
		m = min(max(m, 0), parts)
		return (m*lineTotal + parts - 1) / parts
	}
	return cum(from+n) - cum(from)
}

// MaxSplitParts caps how many people share one item; it matches the CHECK on order_items.split_parts.
const MaxSplitParts = 20

// ErrSplitPaid is returned when a line's split changes after some of its parts were paid.
var ErrSplitPaid = errors.New("item já tem partes pagas")

// SetItemSplit splits each unit of a line into parts for paying by consumption, at any status except fechado
// and cancelado. It is refused once a part of the line was paid by consumption.
func (d *DB) SetItemSplit(ctx context.Context, orderID, itemID, parts int64) error {
	if parts < 1 || parts > MaxSplitParts {
		return ErrInvalid
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		var status string
		var paid int64
		err := tx.QueryRowContext(ctx, `SELECT o.status,
			(SELECT COUNT(*) FROM order_payment_items WHERE order_item_id = oi.id)
			FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE oi.id = ? AND oi.order_id = ?`, itemID, orderID).Scan(&status, &paid)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status == StatusClosed || status == StatusCancelled {
			return ErrBadTransition
		}
		if paid > 0 {
			return ErrSplitPaid
		}
		return execOne(ctx, tx, `UPDATE order_items SET split_parts = ? WHERE id = ?`, parts, itemID)
	})
}

// CanTransition reports whether an order may move from status from to status to.
// It does not check payment; SetStatus does that for fechado.
func CanTransition(from, to, consume string) bool {
	switch from + ">" + to {
	case "aberto>cozinha", "cozinha>pronto", "saiu>fechado",
		"aberto>pronto", // Marcar como pronto: made without a kitchen ticket
		"aberto>cancelado", "cozinha>cancelado", "pronto>cancelado",
		"saiu>cancelado",                     // client cancelled at the last minute or was not home
		"fechado>aberto", "cancelado>aberto": // reopen at any time

		return true
	case "pronto>saiu":
		return consume == ConsumeDelivery
	case "pronto>fechado":
		return consume == ConsumeLocal || consume == ConsumePickup
	}
	return false
}

// ValidConsume reports whether c is one of the consume modes.
func ValidConsume(c string) bool {
	return c == ConsumeDelivery || c == ConsumeLocal || c == ConsumePickup
}

// NewOrder holds the fields set when an order is created.
type NewOrder struct {
	Channel    string
	Consume    string
	ClientName string
	PhoneLast4 string
	Notes      string
	Address    string
}

func (d *DB) CreateOrder(ctx context.Context, o NewOrder) (int64, error) {
	if o.Consume == ConsumeDelivery && o.Address == "" {
		return 0, ErrInvalid
	}
	return insert(ctx, d, `INSERT INTO orders (channel, consume, client_name, phone_last4, notes, address, created_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?)`, o.Channel, o.Consume, o.ClientName, o.PhoneLast4, o.Notes, o.Address, now())
}

const orderCols = `o.id, o.channel, o.consume, o.client_name, COALESCE(o.phone_last4, ''), o.status,
	o.notes, o.address, o.created_at, COALESCE(o.closed_at, ''),
	(SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = o.id),
	(SELECT COALESCE(SUM(amount_cents), 0) FROM order_payments WHERE order_id = o.id),
	(SELECT COUNT(*) FROM order_payments WHERE order_id = o.id), o.split_people,
	COALESCE(o.expected_method, ''), COALESCE(o.change_for_cents, 0)`

func scanOrder(r interface{ Scan(...any) error }, o *Order) error {
	return r.Scan(&o.ID, &o.Channel, &o.Consume, &o.ClientName, &o.PhoneLast4, &o.Status,
		&o.Notes, &o.Address, &o.CreatedAt, &o.ClosedAt, &o.TotalCents, &o.PaidCents, &o.PaymentCount, &o.SplitPeople,
		&o.ExpectedMethod, &o.ChangeForCents)
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
		oi.name_snapshot, oi.note, p.printable_name, oi.split_parts,
		(SELECT COALESCE(SUM(qty), 0) FROM order_payment_items WHERE order_item_id = oi.id)
		FROM order_items oi JOIN products p ON p.id = oi.product_id WHERE oi.order_id = ? ORDER BY oi.id`,
		func(r *sql.Rows, it *OrderItem) error {
			return r.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.Qty, &it.UnitPriceCents, &it.NameSnapshot, &it.Note,
				&it.PrintableName, &it.SplitParts, &it.PaidQty)
		}, id)
	if err != nil {
		return o, err
	}
	o.Payments, err = d.payments(ctx, id)
	return o, err
}

// ListOpenOrders returns orders that are neither fechado nor cancelado, newest first, without items.
func (d *DB) ListOpenOrders(ctx context.Context) ([]Order, error) {
	return queryAll(ctx, d, `SELECT `+orderCols+` FROM orders o
		WHERE o.status NOT IN ('fechado', 'cancelado') ORDER BY o.created_at DESC, o.id DESC`,
		func(r *sql.Rows, o *Order) error { return scanOrder(r, o) })
}

// ListClosedOrders returns the fechado and cancelado orders of [from, to), newest first.
// A fechado order counts on its closed_at; a cancelado one has none, so it counts on its created_at.
func (d *DB) ListClosedOrders(ctx context.Context, from, to time.Time) ([]Order, error) {
	return queryAll(ctx, d, `SELECT `+orderCols+` FROM orders o
		WHERE o.status IN ('fechado', 'cancelado')
		AND COALESCE(o.closed_at, o.created_at) >= ? AND COALESCE(o.closed_at, o.created_at) < ?
		ORDER BY COALESCE(o.closed_at, o.created_at) DESC, o.id DESC`,
		func(r *sql.Rows, o *Order) error { return scanOrder(r, o) },
		from.UTC().Format(timeFormat), to.UTC().Format(timeFormat))
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

// MaxItemNote is the longest note on one order line, in runes.
const MaxItemNote = 100

// SetItemNote changes a line's note on an open order. An empty note clears it.
func (d *DB) SetItemNote(ctx context.Context, orderID, itemID int64, note string) error {
	if utf8.RuneCountInString(note) > MaxItemNote {
		return ErrInvalid
	}
	return d.tx(ctx, func(tx *sql.Tx) error {
		if err := requireOpen(ctx, tx, orderID); err != nil {
			return err
		}
		return execOne(ctx, tx, `UPDATE order_items SET note = ? WHERE id = ? AND order_id = ?`, note, itemID, orderID)
	})
}

// SetCustomer changes the client's name, phone (last 4 digits, may be empty) and the order's note,
// at any status except fechado and cancelado. The handler validates the values.
func (d *DB) SetCustomer(ctx context.Context, id int64, name, phoneLast4, notes string) error {
	if name == "" {
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
		if status == StatusClosed || status == StatusCancelled {
			return ErrBadTransition
		}
		return execOne(ctx, tx, `UPDATE orders SET client_name = ?, phone_last4 = NULLIF(?, ''), notes = ? WHERE id = ?`,
			name, phoneLast4, notes, id)
	})
}

// SetStatus moves an order to a new status after checking the state machine in docs/flows.md.
// Closing needs payment parts that add up to the total, and sending to the kitchen needs at least one item.
func (d *DB) SetStatus(ctx context.Context, id int64, to string) error {
	return d.tx(ctx, func(tx *sql.Tx) error {
		var from, consume string
		var total, paid, parts int64
		err := tx.QueryRowContext(ctx, `SELECT status, consume,
			(SELECT COALESCE(SUM(qty * unit_price_cents), 0) FROM order_items WHERE order_id = orders.id),
			(SELECT COALESCE(SUM(amount_cents), 0) FROM order_payments WHERE order_id = orders.id),
			(SELECT COUNT(*) FROM order_payments WHERE order_id = orders.id)
			FROM orders WHERE id = ?`, id).Scan(&from, &consume, &total, &paid, &parts)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !CanTransition(from, to, consume) {
			return ErrBadTransition
		}
		if to == StatusKitchen || (from == StatusOpen && to == StatusReady) {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_items WHERE order_id = ?`, id).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return ErrEmptyOrder
			}
		}
		if to == StatusClosed {
			if parts == 0 {
				return ErrUnpaid
			}
			if paid != total {
				return ErrPaidMismatch
			}
			return execOne(ctx, tx, `UPDATE orders SET status = ?, closed_at = ? WHERE id = ?`, to, now(), id)
		}
		// Reopening clears closed_at so the order leaves the daily report until it is closed again.
		return execOne(ctx, tx, `UPDATE orders SET status = ?, closed_at = NULL WHERE id = ?`, to, id)
	})
}

// SetConsume switches between entrega, local and retirada while the order is not yet out or finished.
// Entrega needs an address; switching away keeps the old one in case of a mis-tap.
func (d *DB) SetConsume(ctx context.Context, id int64, consume, address string) error {
	if !ValidConsume(consume) || consume == ConsumeDelivery && address == "" {
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
		// Eaten in or picked up, the client pays at the counter: the expected payment is for the courier only.
		return execOne(ctx, tx, `UPDATE orders SET consume = ?, address = COALESCE(NULLIF(?, ''), address),
			expected_method = CASE WHEN ? = 'entrega' THEN expected_method END,
			change_for_cents = CASE WHEN ? = 'entrega' THEN change_for_cents END
			WHERE id = ?`, consume, address, consume, consume, id)
	})
}

// requireOpen allows item edits while the order is aberto, cozinha or pronto, moving it to aberto.
func requireOpen(ctx context.Context, tx *sql.Tx, orderID int64) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM orders WHERE id = ?`, orderID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	switch status {
	case StatusOpen:
		return nil
	case StatusKitchen, StatusReady:
		// Changing items the kitchen already has sends the order back to aberto, to be sent again.
		// The caller's transaction rolls this back if the change itself fails.
		return execOne(ctx, tx, `UPDATE orders SET status = ? WHERE id = ?`, StatusOpen, orderID)
	}
	return ErrNotEditable
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
