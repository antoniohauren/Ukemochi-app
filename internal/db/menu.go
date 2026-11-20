package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// ErrNotFound is returned when a row with the given id does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned when a category name is already taken.
var ErrDuplicate = errors.New("duplicate")

type Category struct {
	ID       int64
	Name     string
	Position int64
	Items    []Item
}

type Item struct {
	ID          int64
	CategoryID  int64
	Name        string
	Description string
	Active      bool
	Products    []Product
}

type Product struct {
	ID            int64
	ItemID        int64
	Variant       string
	Size          string
	PrintableName string
	PriceCents    int64
	Active        bool
}

// DisplayName joins item name, variant and size, e.g. "Coca-Cola Zero Lata".
func DisplayName(item, variant, size string) string {
	parts := []string{item}
	for _, p := range []string{variant, size} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// Menu returns every category with its items and products. With activeOnly, inactive items
// and products are left out, and so are items left with no active product.
func (d *DB) Menu(ctx context.Context, activeOnly bool) ([]Category, error) {
	cats, err := queryAll(ctx, d, `SELECT id, name, position FROM categories ORDER BY position, name`,
		func(r *sql.Rows, c *Category) error { return r.Scan(&c.ID, &c.Name, &c.Position) })
	if err != nil {
		return nil, err
	}
	items, err := queryAll(ctx, d, `SELECT id, category_id, name, description, active FROM items ORDER BY name`,
		func(r *sql.Rows, i *Item) error {
			return r.Scan(&i.ID, &i.CategoryID, &i.Name, &i.Description, &i.Active)
		})
	if err != nil {
		return nil, err
	}
	prods, err := queryAll(ctx, d, `SELECT id, item_id, variant, size, printable_name, price_cents, active
		FROM products ORDER BY price_cents, variant, size`,
		func(r *sql.Rows, p *Product) error {
			return r.Scan(&p.ID, &p.ItemID, &p.Variant, &p.Size, &p.PrintableName, &p.PriceCents, &p.Active)
		})
	if err != nil {
		return nil, err
	}

	byItem := map[int64][]Product{}
	for _, p := range prods {
		if activeOnly && !p.Active {
			continue
		}
		byItem[p.ItemID] = append(byItem[p.ItemID], p)
	}
	byCat := map[int64][]Item{}
	for _, it := range items {
		it.Products = byItem[it.ID]
		if activeOnly && (!it.Active || len(it.Products) == 0) {
			continue
		}
		byCat[it.CategoryID] = append(byCat[it.CategoryID], it)
	}
	for i := range cats {
		cats[i].Items = byCat[cats[i].ID]
	}
	return cats, nil
}

func (d *DB) CreateCategory(ctx context.Context, name string, position int64) (int64, error) {
	id, err := insert(ctx, d, `INSERT INTO categories (name, position) VALUES (?, ?)`, name, position)
	return id, uniqueErr(err)
}

func (d *DB) UpdateCategory(ctx context.Context, id int64, name string) error {
	return uniqueErr(execOne(ctx, d, `UPDATE categories SET name = ? WHERE id = ?`, name, id))
}

// MoveCategory swaps a category with its neighbour above (up) or below, then renumbers
// every category 0..n-1 so ties left by older data never block a move.
func (d *DB) MoveCategory(ctx context.Context, id int64, up bool) error {
	return d.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM categories ORDER BY position, name`)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var v int64
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		i := slices.Index(ids, id)
		if i < 0 {
			return ErrNotFound
		}
		j := i + 1
		if up {
			j = i - 1
		}
		if j >= 0 && j < len(ids) {
			ids[i], ids[j] = ids[j], ids[i]
		}
		for pos, v := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE categories SET position = ? WHERE id = ?`, pos, v); err != nil {
				return err
			}
		}
		return nil
	})
}

// uniqueErr turns a UNIQUE constraint failure into ErrDuplicate.
func uniqueErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrDuplicate
	}
	return err
}

func (d *DB) CreateItem(ctx context.Context, it Item) (int64, error) {
	return insert(ctx, d, `INSERT INTO items (category_id, name, description, active) VALUES (?, ?, ?, ?)`,
		it.CategoryID, it.Name, it.Description, it.Active)
}

func (d *DB) UpdateItem(ctx context.Context, it Item) error {
	return execOne(ctx, d, `UPDATE items SET category_id = ?, name = ?, description = ?, active = ? WHERE id = ?`,
		it.CategoryID, it.Name, it.Description, it.Active, it.ID)
}

func (d *DB) CreateProduct(ctx context.Context, p Product) (int64, error) {
	return insert(ctx, d, `INSERT INTO products (item_id, variant, size, printable_name, price_cents, active)
		VALUES (?, ?, ?, ?, ?, ?)`, p.ItemID, p.Variant, p.Size, p.PrintableName, p.PriceCents, p.Active)
}

// UpdateProduct changes a product. Past orders keep their own name and price snapshot.
func (d *DB) UpdateProduct(ctx context.Context, p Product) error {
	return execOne(ctx, d, `UPDATE products SET variant = ?, size = ?, printable_name = ?, price_cents = ?, active = ?
		WHERE id = ?`, p.Variant, p.Size, p.PrintableName, p.PriceCents, p.Active, p.ID)
}

// queryAll runs a query and scans every row into a slice, closing rows before returning
// so the single pooled connection is free for the next query.
func queryAll[T any](ctx context.Context, d *DB, query string, scan func(*sql.Rows, *T) error, args ...any) ([]T, error) {
	rows, err := d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		var v T
		if err := scan(rows, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func insert(ctx context.Context, d *DB, query string, args ...any) (int64, error) {
	res, err := d.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// execOne runs an UPDATE and returns ErrNotFound when no row matched.
func execOne(ctx context.Context, e interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) error {
	res, err := e.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
