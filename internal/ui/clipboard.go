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
		out, err := exec.Command("pngpaste", "-").Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("no image in clipboard (install pngpaste via brew)")
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
