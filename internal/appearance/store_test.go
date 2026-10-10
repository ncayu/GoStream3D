package appearance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestThemePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "appearance.sqlite")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if theme, err := store.Theme(ctx); err != nil || theme != DefaultTheme {
		t.Fatalf("default: %q %v", theme, err)
	}
	if err := store.SaveTheme(ctx, "emerald"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "light", "BLUE", "blue-tech'; DROP TABLE admin_preferences;--"} {
		if !errors.Is(store.SaveTheme(ctx, invalid), ErrInvalidTheme) {
			t.Fatalf("accepted invalid theme %q", invalid)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Verify this is a SQLite database, not JSON with a new file extension.
	header, err := os.ReadFile(path)
	if err != nil || string(header[:16]) != "SQLite format 3\x00" {
		t.Fatalf("not SQLite: %v", err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if theme, err := store.Theme(ctx); err != nil || theme != "emerald" {
		t.Fatalf("lost saved theme: %q %v", theme, err)
	}
	if err := store.SaveTheme(ctx, DefaultTheme); err != nil {
		t.Fatal(err)
	}
	if theme, err := store.Theme(ctx); err != nil || theme != DefaultTheme {
		t.Fatalf("reset: %q %v", theme, err)
	}
}

func TestLockedDatabaseFailsWithoutChangingTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appearance.sqlite")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	ctx := context.Background()
	transaction, err := first.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "UPDATE admin_preferences SET value = 'rose' WHERE key = 'theme'"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := second.SaveTheme(ctx, "aurora"); err == nil {
		t.Fatal("locked write claimed success")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("database lock exceeded bounded wait")
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	if value, err := second.Theme(ctx); err != nil || value != DefaultTheme {
		t.Fatalf("failed write changed value: %s %v", value, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := second.SaveTheme(cancelled, "glacier"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ignored cancellation: %v", err)
	}
}
