//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

func initConsole() {
	windows.SetConsoleOutputCP(65001)
	windows.SetConsoleCP(65001)

	// Enable ANSI/VT processing so Bubble Tea escape codes render correctly.
	stdout := windows.Handle(windows.Stdout)
	var mode uint32
	windows.GetConsoleMode(stdout, &mode)
	windows.SetConsoleMode(stdout, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
