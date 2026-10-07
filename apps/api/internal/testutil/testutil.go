// Package testutil provides real-Postgres fixtures for integration tests.
//
// Integration tests run when BRIDGE_TEST_DATABASE_URL points at a Postgres
// server the tests may create and drop databases on, for example
// postgres://bridge:bridge@localhost:5432/postgres?sslmode=disable.
package testutil

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bridge/internal/auth"
	"bridge/internal/db"
)

// EnvVar names the admin connection string for integration tests.
const EnvVar = "BRIDGE_TEST_DATABASE_URL"

// Database is a throwaway, fully migrated database.
type Database struct {
	Pool  *pgxpool.Pool
	URL   string
	admin string
	name  string
}

// NewDatabase creates and migrates a fresh database. It returns (nil, nil)
// when EnvVar is unset so callers can skip.
func NewDatabase(ctx context.Context) (*Database, error) {
	admin := os.Getenv(EnvVar)
	if admin == "" {
		return nil, nil
	}
	name := "bridge_test_" + strings.ToLower(auth.RandomString(10))
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", EnvVar, err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return nil, fmt.Errorf("create test database: %w", err)
	}

	u, err := url.Parse(admin)
	if err != nil {
		return nil, err
	}
	u.Path = "/" + name
	pool, err := db.Connect(ctx, u.String(), 10)
	if err != nil {
		return nil, err
	}
	d := &Database{Pool: pool, URL: u.String(), admin: admin, name: name}
	if err := db.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		d.Close(ctx)
		return nil, err
	}
	return d, nil
}

// Close drops the database.
func (d *Database) Close(ctx context.Context) {
	d.Pool.Close()
	conn, err := pgx.Connect(ctx, d.admin)
	if err != nil {
		return
	}
	defer conn.Close(ctx)
	_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{d.name}.Sanitize()+" WITH (FORCE)")
}
