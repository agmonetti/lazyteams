//go:build windows

package version

import (
	"os"
	"strings"
	"testing"
)

func TestConsumeUpdateResult(t *testing.T) {
	for _, tc := range []struct {
		name, result string
		wantError    bool
	}{
		{"success", "ok\n", false},
		{"failure", "Paired update failed: auth helper locked\nRollback failed: access denied", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			marker, err := os.CreateTemp("", "lazyteams-update-result-*.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := marker.WriteString(tc.result); err != nil {
				t.Fatal(err)
			}
			if err := marker.Close(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(marker.Name()) })
			handled, err := ConsumeUpdateResult([]string{"--update-result=" + marker.Name()})
			if !handled || (err != nil) != tc.wantError {
				t.Fatalf("result = %v, %v", handled, err)
			}
			if tc.wantError && !strings.Contains(err.Error(), "Rollback failed") {
				t.Errorf("missing rollback detail: %v", err)
			}
			if _, err := os.Stat(marker.Name()); !os.IsNotExist(err) {
				t.Errorf("result marker remains: %v", err)
			}
		})
	}
}

func TestConsumeUpdateResultDoesNotReadArbitraryFile(t *testing.T) {
	private, err := os.CreateTemp("", "private-user-data-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(private.Name())
	if _, err := private.WriteString("leave intact"); err != nil {
		t.Fatal(err)
	}
	if err := private.Close(); err != nil {
		t.Fatal(err)
	}
	handled, err := ConsumeUpdateResult([]string{"--update-result=" + private.Name()})
	if !handled || err == nil {
		t.Fatalf("arbitrary marker = %v, %v", handled, err)
	}
	if data, err := os.ReadFile(private.Name()); err != nil || string(data) != "leave intact" {
		t.Errorf("user file changed: %q, %v", data, err)
	}
}
