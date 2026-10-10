package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestAdminCredentialsPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "admin-account.json")
	if saved, err := loadAdminCredentials(path); err != nil || saved != nil {
		t.Fatalf("fresh installation: %v %v", saved, err)
	}
	password := "new-private-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	credentials := adminCredentials{Version: 1, Username: "operator", PasswordHash: string(hash)}
	for _, username := range []string{"operator", "new-operator"} {
		credentials.Username = username
		if err := saveAdminCredentials(path, credentials); err != nil {
			t.Fatal(err)
		}
		saved, err := loadAdminCredentials(path)
		if err != nil || saved.Username != username {
			t.Fatalf("readback: %v %v", saved, err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte(password)); err != nil {
			t.Fatal(err)
		}
	}
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), password) {
		t.Fatal("plaintext password persisted")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"username":"operator","password_hash":"broken"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdminCredentials(path); err == nil {
		t.Fatal("corrupt account silently accepted")
	}
}

func TestAdminUsernameValidation(t *testing.T) {
	for _, username := range []string{"admin", "operator-2", "管理员"} {
		if !validAdminUsername(username) {
			t.Fatalf("valid username rejected: %q", username)
		}
	}
	for _, username := range []string{"", " admin", "a b", "a\nb", strings.Repeat("a", 65), string([]byte{0xff})} {
		if validAdminUsername(username) {
			t.Fatalf("invalid username accepted: %q", username)
		}
	}
}

func TestSavedAdminPasswordDoesNotAcceptBcryptTruncation(t *testing.T) {
	password := strings.Repeat("a", 72)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	auth := newAdminAuth("admin", "initial-private-password", "")
	auth.savedPasswordHash = hash
	if !auth.passwordMatchesLocked(password) || auth.passwordMatchesLocked(password+"x") {
		t.Fatal("saved password comparison accepted truncation or rejected the valid boundary")
	}
}
