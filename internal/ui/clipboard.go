package ui

import (
	"fmt"
	"os/exec"
	"runtime"
)

// getClipboardImage tries to extract an image from the system clipboard
func getClipboardImage() ([]byte, error) {
	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("wl-paste", "-t", "image/png").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		out, err = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("no image in clipboard (install wl-paste or xclip)")
	case "darwin":
		// Try pngpaste first if installed (faster)
		out, err := exec.Command("pngpaste", "-").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}

		// Native fallback using osascript — no external tools required.
		// osascript prints raw data bytes of the PNG to stdout.
		out, err = exec.Command("osascript", "-e", "the clipboard as «class PNGf»").Output()
		if err == nil && isValidPNG(out) {
			return out, nil
		}
		return nil, fmt.Errorf("no image in clipboard")
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; $img = [System.Windows.Forms.Clipboard]::GetImage(); if ($img -eq $null) { exit 1 }; $ms = New-Object System.IO.MemoryStream; $img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); [Console]::OpenStandardOutput().Write($ms.ToArray(), 0, $ms.Length)`
		out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("no image in clipboard")
	default:
		return nil, fmt.Errorf("clipboard image pasting not supported on this OS")
	}
}

// isValidPNG checks the PNG magic bytes (89 50 4E 47 0D 0A 1A 0A).
func isValidPNG(data []byte) bool {
	return len(data) >= 8 &&
		data[0] == 0x89 &&
		data[1] == 'P' &&
		data[2] == 'N' &&
		data[3] == 'G' &&
		data[4] == '\r' &&
		data[5] == '\n' &&
		data[6] == 0x1a &&
		data[7] == '\n'
}
