// Package store persists metadata (clusters, history, reports) in pure-Go SQLite.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Store owns the SQLite connection pool.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. The file is created readable by the owner only because it holds
// cluster credentials.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: create data directory: %w", err)
	}
	if err := ensurePrivateFile(path); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open database: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.DB.Close() }

func ensurePrivateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("store: create database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("store: close database file: %w", err)
	}
	return nil
}

// migrate applies every migration newer than the database's user_version.
func migrate(db *sql.DB) error {
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}

	for version := current; version < len(migrations); version++ {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", version+1, err)
		}
		if _, err := tx.Exec(migrations[version]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: apply migration %d: %w", version+1, err)
		}
		// PRAGMA does not accept bound parameters; the value is a trusted integer.
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: record migration %d: %w", version+1, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", version+1, err)
		}
	}
	return nil
}
