package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

//go:embed schema.sql
var schema string

var stepMarker = regexp.MustCompile(`(?m)^-- migrate: (\d+)\s*$`)

type step struct {
	version int
	sql     string
}

// steps splits schema.sql at its "-- migrate: N" markers. Versions must count up from 1.
func steps(src string) ([]step, error) {
	locs := stepMarker.FindAllStringSubmatchIndex(src, -1)
	out := make([]step, 0, len(locs))
	for i, loc := range locs {
		v, _ := strconv.Atoi(src[loc[2]:loc[3]])
		if v != i+1 {
			return nil, fmt.Errorf("schema.sql: migration %d out of order, want %d", v, i+1)
		}
		end := len(src)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, step{v, src[loc[1]:end]})
	}
	return out, nil
}

// Migrate applies every schema step above the current PRAGMA user_version, each in its own transaction.
// Steps run with foreign_keys off, so a step may rebuild a referenced table (SQLite can't alter a CHECK);
// PRAGMA foreign_key_check runs before each commit instead.
func (d *DB) Migrate(ctx context.Context) error {
	all, err := steps(schema)
	if err != nil {
		return err
	}
	// Pin one connection: the foreign_keys pragma is per connection and a no-op inside a transaction.
	conn, err := d.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var current int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(all) {
		return fmt.Errorf("database version %d is newer than this app (%d)", current, len(all))
	}
	if current == len(all) {
		return nil
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys = ON")
	for _, s := range all[current:] {
		if err := applyStep(ctx, conn, s); err != nil {
			return fmt.Errorf("migration %d: %w", s.version, err)
		}
	}
	return nil
}

func applyStep(ctx context.Context, conn *sql.Conn, s step) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.sql); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	broken := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if broken {
		return errors.New("foreign key check failed")
	}
	// PRAGMA takes no placeholders; the version is an int from our own schema, not input.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", s.version)); err != nil {
		return err
	}
	return tx.Commit()
}
