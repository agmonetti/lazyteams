//go:build !windows

package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "dest")
	if err := os.WriteFile(src, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-binary" {
		t.Errorf("dest = %q, want new-binary", data)
	}
	// Source is left in place (we copy, not move), so it can be reused.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should still exist after replace: %v", err)
	}
	// Executable permissions preserved.
	info, _ := os.Stat(dest)
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("dest should remain executable")
	}
}

func TestReplaceBinaryMissingSource(t *testing.T) {
	dir := t.TempDir()
	if err := ReplaceBinary(filepath.Join(dir, "nope"), filepath.Join(dir, "x")); err == nil {
		t.Error("expected error for missing source")
	}
}

// TestReplaceBinaryForcesExecutable covers the real updater scenario: binaries
// are downloaded into non-executable temp files (0600), and the destination
// must still end up executable.
func TestReplaceBinaryForcesExecutable(t *testing.T) {
	dir := t.TempDir()
	// src has 0600, like an os.CreateTemp download temp.
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "lazyteams")
	if err := os.WriteFile(src, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	// Even though src is 0600, dest must remain executable.
	info, _ := os.Stat(dest)
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("dest mode = %v, want executable", info.Mode().Perm())
	}
	// And the previous 0755 mode should be preserved.
	if info.Mode().Perm() != 0o755 {
		t.Errorf("dest mode = %v, want 0755", info.Mode().Perm())
	}
}

// TestReplaceBinaryDefaultsExecutable covers a fresh destination with no
// existing mode; it must default to 0755.
func TestReplaceBinaryDefaultsExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dest := filepath.Join(dir, "lazyteams") // does not exist yet
	if err := os.WriteFile(src, []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(src, dest); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(dest)
	if info.Mode().Perm() != 0o755 {
		t.Errorf("dest mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestExecutablePath(t *testing.T) {
	p, err := ExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Error("ExecutablePath returned empty")
	}
}
