//go:build !windows

package version

import "testing"

func TestExecutablePath(t *testing.T) {
	p, err := ExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Error("ExecutablePath returned empty")
	}
}
