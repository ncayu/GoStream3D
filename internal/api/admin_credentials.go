package api

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

type adminCredentials struct {
	Version      int    `json:"version"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
}

func validAdminUsername(username string) bool {
	if !utf8.ValidString(username) || strings.TrimSpace(username) != username || username == "" || utf8.RuneCountInString(username) > 64 {
		return false
	}
	return !strings.ContainsFunc(username, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) })
}

func loadAdminCredentials(path string) (*adminCredentials, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	var credentials adminCredentials
	if err := decoder.Decode(&credentials); err != nil {
		return nil, errors.New("invalid saved administrator account")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("invalid saved administrator account")
	}
	cost, err := bcrypt.Cost([]byte(credentials.PasswordHash))
	if credentials.Version != 1 || !validAdminUsername(credentials.Username) || len(credentials.PasswordHash) != 60 || err != nil || cost < bcrypt.DefaultCost || cost > 14 {
		return nil, errors.New("invalid saved administrator account")
	}
	return &credentials, nil
}

func saveAdminCredentials(path string, credentials adminCredentials) error {
	if path == "" {
		return errors.New("administrator account storage is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".admin-account-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(credentials); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
