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

func TestExecutablePath(t *testing.T) {
	p, err := ExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Error("ExecutablePath returned empty")
	}
}
