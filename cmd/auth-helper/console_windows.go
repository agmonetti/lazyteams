//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

func initConsole() {
	// Windows console defaults to CP437/CP850; switch to UTF-8 so that
	// accented characters and checkmarks (✓) render correctly.
	windows.SetConsoleOutputCP(65001)
	windows.SetConsoleCP(65001)
}
