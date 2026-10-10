// Package appearance persists the shared administrator console theme in SQLite.
package appearance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const DefaultTheme = "blue-tech"

var ErrInvalidTheme = errors.New("invalid console theme")

func ValidTheme(theme string) bool {
	switch theme {
	case DefaultTheme, "deep-ocean", "aurora", "neon-cyan", "sunset", "rose", "emerald", "graphite", "gold", "glacier":
		return true
	}
	return false
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Keep one connection so the bounded busy timeout applies to every operation.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `PRAGMA busy_timeout = 1000;
		CREATE TABLE IF NOT EXISTS admin_preferences (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO admin_preferences (key, value) VALUES ('theme', 'blue-tech') ON CONFLICT(key) DO NOTHING;`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize appearance database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Theme(ctx context.Context) (string, error) {
	var theme string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM admin_preferences WHERE key = 'theme'").Scan(&theme); err != nil {
		return "", err
	}
	if !ValidTheme(theme) {
		return "", ErrInvalidTheme
	}
	return theme, nil
}

func (s *Store) SaveTheme(ctx context.Context, theme string) error {
	if !ValidTheme(theme) {
		return ErrInvalidTheme
	}
	// The response is sent only after SQLite has committed this atomic statement.
	_, err := s.db.ExecContext(ctx, "INSERT INTO admin_preferences (key, value) VALUES ('theme', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", theme)
	return err
}
