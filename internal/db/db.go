// Package db owns the SQLite database: schema, migrations and queries.
package db

import (
	"context"
	"database/sql"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite handle. Query functions are added as methods in feature files.
type DB struct {
	*sql.DB
}

// Open opens the SQLite database at path.
func Open(path string) (*DB, error) {
	// shortcut: assumes path has no query string of its own; merge params if that changes.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes writes, so concurrent requests never hit SQLITE_BUSY.
	sqlDB.SetMaxOpenConns(1)
	d := &DB{sqlDB}
	if err := d.Migrate(context.Background()); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}
