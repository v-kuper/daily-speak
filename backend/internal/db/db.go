package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if requireSSL {
		config.ConnConfig.TLSConfig = &tlsRejectUnauthorizedFalse
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

func (d *DB) Close() {
	if d != nil && d.pool != nil {
		d.pool.Close()
	}
}

func (d *DB) Migrate(ctx context.Context) error {
	if d == nil || d.pool == nil {
		return errors.New("database is not configured")
	}
	_, err := d.pool.Exec(ctx, InitialSchemaSQL())
	return err
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
