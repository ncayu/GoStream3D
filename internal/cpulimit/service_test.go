package cpulimit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLimitAppliesImmediatelyAndRestoresOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu-settings.json")
	actual := 0
	apply := func(percent int) error { actual = percent; return nil }
	s, err := New(path, 50, apply)
	if err != nil {
		t.Fatal(err)
	}
	if actual != 50 {
		t.Fatal("initial cap missing")
	}
	if err := s.SetPercent(40); err != nil {
		t.Fatal(err)
	}
	if actual != 40 || s.Settings().Percent != 40 {
		t.Fatal("runtime cap not updated")
	}
	for _, invalid := range []int{-1, 0, 101} {
		if err := s.SetPercent(invalid); !errors.Is(err, ErrInvalidPercent) || actual != 40 {
			t.Fatalf("invalid cap changed protection: %v", err)
		}
	}
	actual = 0
	restarted, err := New(path, 50, apply)
	if err != nil || actual != 40 || restarted.Settings().Percent != 40 {
		t.Fatalf("saved setting lost: actual=%d err=%v", actual, err)
	}
}

func TestApplyFailureLeavesSavedAndActiveLimitUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu-settings.json")
	actual := 0
	apply := func(percent int) error {
		if percent == 75 {
			return errors.New("OS refused limit")
		}
		actual = percent
		return nil
	}
	s, err := New(path, 50, apply)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPercent(40); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPercent(75); err == nil || actual != 40 || s.Settings().Percent != 40 {
		t.Fatalf("failed update altered cap: %v", err)
	}
	restarted, err := New(path, 50, apply)
	if err != nil || restarted.Settings().Percent != 40 {
		t.Fatalf("failed update was persisted: %v", err)
	}
}

func TestCommitFailureRestoresPriorRuntimeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu-settings.json")
	actual := 0
	apply := func(percent int) error { actual = percent; return nil }
	s, err := New(path, 50, apply)
	if err != nil {
		t.Fatal(err)
	}
	// Force only the atomic rename to fail, after staging and OS apply.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPercent(75); err == nil || actual != 50 || s.Settings().Percent != 50 {
		t.Fatalf("commit failure did not restore cap: %v", err)
	}
}

func TestBadSavedLimitFailsStartup(t *testing.T) {
	for _, contents := range []string{`{"cpu_max_percent":0}`, `{"cpu_max_percent":101}`, `{}`, `{not-json}`} {
		path := filepath.Join(t.TempDir(), "cpu-settings.json")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		applied := false
		if _, err := New(path, 50, func(int) error { applied = true; return nil }); err == nil || applied {
			t.Fatal("bad settings silently fell back to a higher cap")
		}
	}
}
