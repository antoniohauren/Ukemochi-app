package db

import (
	"context"
	_ "embed"
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
func (d *DB) Migrate(ctx context.Context) error {
	all, err := steps(schema)
	if err != nil {
		return err
	}
	var current int
	if err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current > len(all) {
		return fmt.Errorf("database version %d is newer than this app (%d)", current, len(all))
	}
	for _, s := range all[current:] {
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, s.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", s.version, err)
		}
		// PRAGMA takes no placeholders; the version is an int from our own schema, not input.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", s.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", s.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: %w", s.version, err)
		}
	}
	return nil
}
