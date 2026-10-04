package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/001_initial.sql
var initialSQL string

type Store struct{ DB *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;"); err != nil {
		return fail(fmt.Errorf("database pragmas: %w", err))
	}
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(fmt.Errorf("schema version: %w", err))
	}
	if version > 1 {
		return fail(fmt.Errorf("database schema %d is newer than this server", version))
	}
	if version == 0 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fail(err)
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, initialSQL); err != nil {
			return fail(fmt.Errorf("migration: %w", err))
		}
		if err = tx.Commit(); err != nil {
			return fail(fmt.Errorf("commit migration: %w", err))
		}
		slog.Info("database migration", "version", 1)
	}
	return &Store{DB: db}, nil
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
