// Package store is a thin SQL repository layer over SQLite. No ORM.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

// Open opens (or creates) the SQLite database at path with WAL + foreign keys on.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// ponytail: SQLite writes serialize anyway; one conn avoids "database is locked" surprises.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func today() string { return time.Now().UTC().Format("2006-01-02") }
