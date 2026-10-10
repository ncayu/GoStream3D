package cpulimit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCgroupQuotaUsesWholeMachineCapacityAndRestores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cpu.max")
	if err := os.WriteFile(path, []byte("max 100000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := NewCgroupController(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(filepath.Join(dir, "settings.json"), 0, controller.Apply)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPercent(25); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.TrimSpace(string(raw)) != "400000 100000" {
		t.Fatal(string(raw))
	}
	svc, err = New(filepath.Join(dir, "settings.json"), 0, controller.Apply)
	if err != nil || svc.Settings().Percent != 25 {
		t.Fatal(err)
	}
	if err := svc.SetPercent(1); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.TrimSpace(string(raw)) != "16000 100000" {
		t.Fatal(string(raw))
	}
}

func TestCgroupRejectsMissingControllerAndInvalidQuotas(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cpu.max")
	if _, err := NewCgroupController(path, 8); err == nil {
		t.Fatal("missing controller accepted")
	}
	os.WriteFile(path, []byte("max 100000\n"), 0600)
	c, err := NewCgroupController(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, percent := range []int{-1, 101} {
		if c.Apply(percent) == nil {
			t.Fatal(percent)
		}
	}
	if c.Apply(1) != nil {
		t.Fatal("1% of one core must be valid")
	}
	raw, _ := os.ReadFile(path)
	if strings.TrimSpace(string(raw)) != "1000 100000" {
		t.Fatal(string(raw))
	}
}
