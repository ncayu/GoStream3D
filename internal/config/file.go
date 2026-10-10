package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadFile reads KEY=VALUE settings. Environment variables take precedence.
// A missing default file is allowed so existing environment-only deployments work.
func LoadFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) && path == "config.env" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		entry := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || !validKey(key) {
			return fmt.Errorf("config %q line %d: expected GOSTREAM3D_KEY=value", path, line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("config %q line %d: %w", path, line, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read config %q: %w", path, err)
	}
	return nil
}

func validKey(key string) bool {
	if !strings.HasPrefix(key, "GOSTREAM3D_") || len(key) == len("GOSTREAM3D_") {
		return false
	}
	for _, r := range key {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
