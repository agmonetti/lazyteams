package ui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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

		// Native JXA fallback using NSPasteboard — no external tools required.
		// Most macOS apps publish public.tiff (not public.png), so we read both
		// types and convert TIFF to PNG with sips when needed.
		out, err = exec.Command("osascript", "-l", "JavaScript", "-e", `
ObjC.import('AppKit');
const pb = $.NSPasteboard.generalPasteboard;
const png = pb.dataForType('public.png');
if (png) {
    'PNG:' + png.base64EncodedStringWithOptions(0).js;
} else {
    const tiff = pb.dataForType('public.tiff');
    if (tiff) {
        'TIFF:' + tiff.base64EncodedStringWithOptions(0).js;
    } else {
        '';
    }
}`).Output()
		if err != nil {
			return nil, fmt.Errorf("no image in clipboard")
		}
		encoded := strings.TrimSpace(string(out))
		if encoded == "" {
			return nil, fmt.Errorf("no image in clipboard")
		}

		switch {
		case strings.HasPrefix(encoded, "PNG:"):
			data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "PNG:"))
			if err == nil && isValidPNG(data) {
				return data, nil
			}
			return nil, fmt.Errorf("no image in clipboard")
		case strings.HasPrefix(encoded, "TIFF:"):
			data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, "TIFF:"))
			if err != nil {
				return nil, fmt.Errorf("no image in clipboard")
			}
			return convertTIFFToPNG(data)
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

// convertTIFFToPNG converts raw TIFF bytes to PNG using the system sips tool.
// Temp files are always removed so a failed conversion never leaks into /tmp.
func convertTIFFToPNG(tiff []byte) ([]byte, error) {
	convertErr := func(err error) ([]byte, error) {
		return nil, fmt.Errorf("failed to convert clipboard TIFF to PNG: %w", err)
	}

	in, err := os.CreateTemp("", "lazyteams-clipboard-*.tiff")
	if err != nil {
		return convertErr(err)
	}
	inName := in.Name()
	defer os.Remove(inName)
	if _, err := in.Write(tiff); err != nil {
		in.Close()
		return convertErr(err)
	}
	if err := in.Close(); err != nil {
		return convertErr(err)
	}

	out, err := os.CreateTemp("", "lazyteams-clipboard-*.png")
	if err != nil {
		return convertErr(err)
	}
	outName := out.Name()
	out.Close()
	defer os.Remove(outName)

	if err := exec.Command("sips", "-s", "format", "png", inName, "--out", outName).Run(); err != nil {
		return convertErr(err)
	}

	data, err := os.ReadFile(outName)
	if err != nil {
		return convertErr(err)
	}
	if !isValidPNG(data) {
		return convertErr(fmt.Errorf("converted output is not a valid PNG"))
	}
	return data, nil
}
