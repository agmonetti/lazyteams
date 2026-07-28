package ui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// getClipboardImage tries to extract an image from the system clipboard
// using standard Linux tools (wl-paste for Wayland, xclip for X11) or macOS pbpaste equivalents.
func getClipboardImage() ([]byte, error) {
	if runtime.GOOS == "linux" {
		// Try Wayland first
		out, err := exec.Command("wl-paste", "-t", "image/png").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		// Try X11
		out, err = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("no image found in clipboard or wl-paste/xclip not installed")
	} else if runtime.GOOS == "darwin" {
		// Try pngpaste on macOS (must be installed via brew)
		out, err := exec.Command("pngpaste", "-").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("no image found in clipboard or pngpaste not installed")
	}
	return nil, fmt.Errorf("clipboard image pasting not supported on this OS")
}
