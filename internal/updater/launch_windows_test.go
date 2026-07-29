//go:build windows

package updater

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyWindowsUpdateReplacesAndLaunchesExactTarget(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "downloaded.exe")
	target := filepath.Join(directory, "custom", "Renamed-Entcoin.exe")
	if err := os.Mkdir(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	writeUpdateTestFile(t, source, "new executable")
	writeUpdateTestFile(t, target, "old executable")
	launched := ""

	err := applyWindowsUpdate(source, target, func(path string) error {
		launched = path
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if launched != target {
		t.Fatalf("launched %q, want exact target %q", launched, target)
	}
	assertUpdateTestFile(t, target, "new executable")
	if _, err := os.Stat(target + ".update-backup"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup remains after successful update: %v", err)
	}
}

func TestApplyWindowsUpdateRollsBackWhenRelaunchFails(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "downloaded.exe")
	target := filepath.Join(directory, "portable", "Entcoin.exe")
	if err := os.Mkdir(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	writeUpdateTestFile(t, source, "new executable")
	writeUpdateTestFile(t, target, "old executable")

	err := applyWindowsUpdate(source, target, func(string) error {
		return errors.New("launch denied")
	})
	if err == nil || !strings.Contains(err.Error(), "launch denied") {
		t.Fatalf("error = %v, want launch failure", err)
	}
	assertUpdateTestFile(t, target, "old executable")
	if _, err := os.Stat(target + ".update-backup"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup remains after rollback: %v", err)
	}
}

func TestHandleUpdateHelperRejectsWrongVersionBeforeWaiting(t *testing.T) {
	handled, err := HandleUpdateHelper([]string{updateHelperArgument, "123", `C:\\Entcoin.exe`, "9.9.9"})
	if !handled || err == nil || !strings.Contains(err.Error(), "expected 9.9.9") {
		t.Fatalf("handled = %v, error = %v", handled, err)
	}
}

func writeUpdateTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertUpdateTestFile(t *testing.T, path, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != expected {
		t.Fatalf("%s = %q, want %q", path, contents, expected)
	}
}
