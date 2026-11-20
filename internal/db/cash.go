package db

import (
	"context"
	"database/sql"
	"errors"
)

// OpeningCash returns the cash the drawer started day (YYYY-MM-DD) with, 0 when not set.
func (d *DB) OpeningCash(ctx context.Context, day string) (int64, error) {
	var cents int64
	err := d.QueryRowContext(ctx, `SELECT opening_cents FROM cash_days WHERE day = ?`, day).Scan(&cents)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return cents, err
}

// SetOpeningCash sets the opening cash of day (YYYY-MM-DD), replacing any earlier value.
func (d *DB) SetOpeningCash(ctx context.Context, day string, cents int64) error {
	if cents < 0 {
		return ErrInvalid
	}
	_, err := d.ExecContext(ctx, `INSERT INTO cash_days (day, opening_cents) VALUES (?, ?)
		ON CONFLICT (day) DO UPDATE SET opening_cents = excluded.opening_cents`, day, cents)
	return err
}
