package ui

import (
	"fmt"
	"os"
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
		// Use a unique temp file per call to avoid races on rapid Ctrl+P.
		tmpFile, err := os.CreateTemp("", "msTTui-clipboard-*.png")
		if err != nil {
			return nil, fmt.Errorf("no image in clipboard")
		}
		tmpPath := tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(tmpPath)

		script := fmt.Sprintf(`
        set tmpFile to "%s"
        try
            set imgData to (the clipboard as «class PNGf»)
            set f to open for access POSIX file tmpFile with write permission
            set eof of f to 0
            write imgData to f
            close access f
        on error
            try
                close access POSIX file tmpFile
            end try
            error "No PNG image in clipboard"
        end try
    `, tmpPath)

		cmd := exec.Command("osascript", "-e", script)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("no image in clipboard")
		}

		data, err := os.ReadFile(tmpPath)
		if err != nil || len(data) == 0 {
			return nil, fmt.Errorf("no image in clipboard")
		}
		return data, nil
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
