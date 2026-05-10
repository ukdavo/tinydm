// Package db provides database connection management and migrations for TinyDM.
// It supports SQLite (the default) and PostgreSQL as alternative backends.
//
// Both drivers are always compiled into the binary; the active backend is
// selected at runtime via TINYDM_DB_DRIVER.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"regexp"
	"strconv"

	"github.com/pressly/goose/v3"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver ("pgx")
	_ "modernc.org/sqlite"              // SQLite driver ("sqlite")
)

//go:embed migrations/*.sql
var sqliteMigrations embed.FS

//go:embed migrations_pg/*.sql
var postgresMigrations embed.FS

// questionRe matches bare ? placeholders (SQL parameter markers).
var questionRe = regexp.MustCompile(`\?`)

// rebind converts ? placeholders to $N for PostgreSQL. For SQLite the query
// is returned unchanged.
func rebind(driver, query string) string {
	if driver != "postgres" {
		return query
	}
	var n int
	return questionRe.ReplaceAllStringFunc(query, func(string) string {
		n++
		return "$" + strconv.Itoa(n)
	})
}

// ─── DB ───────────────────────────────────────────────────────────────────────

// DB wraps *sql.DB and transparently rebinds ? placeholders to $N when the
// backend is PostgreSQL. All embedded *sql.DB methods are promoted; only the
// query-execution methods are shadowed.
type DB struct {
	*sql.DB
	driver string // "sqlite" or "postgres"
}

// Driver returns the name of the active backend ("sqlite" or "postgres").
func (d *DB) Driver() string { return d.driver }

// QueryContext rebinds placeholders and delegates to the underlying DB.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, rebind(d.driver, query), args...)
}

// QueryRowContext rebinds placeholders and delegates to the underlying DB.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, rebind(d.driver, query), args...)
}

// ExecContext rebinds placeholders and delegates to the underlying DB.
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, rebind(d.driver, query), args...)
}

// BeginTx starts a transaction and returns a wrapped *Tx that also rebinds.
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, driver: d.driver}, nil
}

// ─── Tx ───────────────────────────────────────────────────────────────────────

// Tx wraps *sql.Tx with automatic placeholder rebinding.
type Tx struct {
	*sql.Tx
	driver string
}

// ExecContext rebinds placeholders and delegates to the underlying Tx.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, rebind(t.driver, query), args...)
}

// QueryContext rebinds placeholders and delegates to the underlying Tx.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, rebind(t.driver, query), args...)
}

// QueryRowContext rebinds placeholders and delegates to the underlying Tx.
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, rebind(t.driver, query), args...)
}

// ─── Open ─────────────────────────────────────────────────────────────────────

// Open opens (or creates) a database for the given driver and DSN.
//
//   - driver "sqlite": dsn is a file path (e.g. "tinydm.db").
//   - driver "postgres": dsn is a libpq connection string
//     (e.g. "host=localhost user=tinydm dbname=tinydm sslmode=disable").
func Open(driver, dsn string) (*DB, error) {
	var sqlDriver string
	switch driver {
	case "sqlite":
		sqlDriver = "sqlite"
	case "postgres":
		sqlDriver = "pgx"
	default:
		return nil, fmt.Errorf("unknown database driver %q: use sqlite or postgres", driver)
	}

	sqlDB, err := sql.Open(sqlDriver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db := &DB{DB: sqlDB, driver: driver}

	if driver == "sqlite" {
		// SQLite is single-writer; a pool of 1 avoids SQLITE_BUSY under WAL.
		db.SetMaxOpenConns(1)

		pragmas := []string{
			"PRAGMA journal_mode=WAL",   // write-ahead log — better read concurrency
			"PRAGMA foreign_keys=ON",    // enforce FK constraints
			"PRAGMA busy_timeout=5000",  // wait up to 5 s instead of SQLITE_BUSY
			"PRAGMA synchronous=NORMAL", // safe with WAL, faster than FULL
		}
		for _, p := range pragmas {
			if _, err := sqlDB.ExecContext(context.Background(), p); err != nil {
				return nil, fmt.Errorf("set pragma %q: %w", p, err)
			}
		}
	}

	return db, nil
}

// ─── Migrate ──────────────────────────────────────────────────────────────────

// Migrate runs all pending SQL migrations using goose, selecting the migration
// set that matches the active driver.
func Migrate(db *DB) error {
	switch db.driver {
	case "sqlite":
		goose.SetBaseFS(sqliteMigrations)
		if err := goose.SetDialect("sqlite3"); err != nil {
			return fmt.Errorf("set dialect: %w", err)
		}
		if err := goose.Up(db.DB, "migrations"); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
	case "postgres":
		goose.SetBaseFS(postgresMigrations)
		if err := goose.SetDialect("postgres"); err != nil {
			return fmt.Errorf("set dialect: %w", err)
		}
		if err := goose.Up(db.DB, "migrations_pg"); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
	default:
		return fmt.Errorf("unknown driver %q", db.driver)
	}
	return nil
}
