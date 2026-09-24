package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"daily-speaking-practice/backend/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

func InitialSchemaSQL() string {
	return migrations.InitialSchema
}

func Connect(ctx context.Context, databaseURL string, requireSSL bool) (*DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("DATABASE_URL is required to use PostgreSQL")
	}
	config, err := parsePoolConfig(databaseURL, requireSSL)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{pool: pool}, nil
}

func parsePoolConfig(databaseURL string, requireSSL bool) (*pgxpool.Config, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if requireSSL {
		if err := requireVerifiedTLS(config); err != nil {
			return nil, err
		}
	}
	return config, nil
}

func requireVerifiedTLS(config *pgxpool.Config) error {
	tlsConfig := config.ConnConfig.TLSConfig
	if tlsConfig == nil || tlsConfig.InsecureSkipVerify || strings.TrimSpace(tlsConfig.ServerName) == "" {
		return errors.New("DATABASE_SSL requires sslmode=verify-full with a trusted PostgreSQL certificate")
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback == nil || fallback.TLSConfig == nil || fallback.TLSConfig.InsecureSkipVerify || strings.TrimSpace(fallback.TLSConfig.ServerName) == "" {
			return errors.New("DATABASE_SSL requires sslmode=verify-full for every PostgreSQL fallback")
		}
	}
	return nil
}

func (d *DB) Close() {
	if d != nil && d.pool != nil {
		d.pool.Close()
	}
}

func (d *DB) Migrate(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return errors.New("database is not configured")
	}
	catalog, err := migrations.All()
	if err != nil {
		return err
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	// A transaction-scoped lock serializes migrations across API replicas.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(3144518801)); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT name, checksum FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read migration ledger: %w", err)
	}
	applied := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration ledger: %w", err)
		}
		applied[name] = checksum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate migration ledger: %w", err)
	}
	rows.Close()
	pending, err := pendingMigrations(catalog, applied)
	if err != nil {
		return err
	}
	for _, migration := range pending {
		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name, checksum) VALUES ($1, $2)`, migration.Name, migration.Checksum); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func pendingMigrations(catalog []migrations.Migration, applied map[string]string) ([]migrations.Migration, error) {
	known := make(map[string]bool, len(catalog))
	firstPending := len(catalog)
	for index, migration := range catalog {
		known[migration.Name] = true
		checksum, ok := applied[migration.Name]
		if !ok {
			if firstPending == len(catalog) {
				firstPending = index
			}
			continue
		}
		if checksum != migration.Checksum {
			return nil, fmt.Errorf("migration %s checksum mismatch", migration.Name)
		}
		if firstPending < index {
			return nil, fmt.Errorf("migration %s applied after missing migration", migration.Name)
		}
	}
	for name := range applied {
		if !known[name] {
			return nil, fmt.Errorf("unknown applied migration %s", name)
		}
	}
	return catalog[firstPending:], nil
}

func (d *DB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if d == nil || d.pool == nil {
		return nil, errors.New("database is not configured")
	}
	return d.pool.Query(ctx, sql, args...)
}

func (d *DB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if d == nil || d.pool == nil {
		return errorRow{err: errors.New("database is not configured")}
	}
	return d.pool.QueryRow(ctx, sql, args...)
}

func (d *DB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if d == nil || d.pool == nil {
		return pgconn.CommandTag{}, errors.New("database is not configured")
	}
	return d.pool.Exec(ctx, sql, args...)
}

func (d *DB) Begin(ctx context.Context) (pgx.Tx, error) {
	if d == nil || d.pool == nil {
		return nil, errors.New("database is not configured")
	}
	return d.pool.Begin(ctx)
}

func SchemaHash() string {
	sum := sha256.Sum256([]byte(InitialSchemaSQL()))
	return hex.EncodeToString(sum[:])
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(dest ...any) error {
	return r.err
}
