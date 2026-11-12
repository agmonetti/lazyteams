package ui

import (
	"os"
	"testing"
)

// Tests exercise code that saves preferences and credentials under the home
// directory; point it at a scratch directory so they never touch a real install.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "lazyteams-ui-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
