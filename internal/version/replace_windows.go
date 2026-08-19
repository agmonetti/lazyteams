//go:build windows

package version

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ReplaceBinary replaces dest with the content of src on Windows. Because a
// running .exe cannot be overwritten, we stage a detached helper that waits
// for our process to exit, then swaps the files and relaunches the app.
// The caller is expected to exit promptly after this returns (the updater
// exits once the replacement is staged and the restart is delegated).
func ReplaceBinary(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := info.Mode()

	// Write the new binary next to the target under a staging name.
	staged := dest + ".new"
	if err := copyFile(src, staged); err != nil {
		return fmt.Errorf("staging %s: %w", dest, err)
	}
	if err := os.Chmod(staged, mode); err != nil {
		return err
	}

	// Launch a detached stager: wait, move the new exe over the old one,
	// then re-launch it and exit. Using cmd /c lets us defer until after the
	// current process has released the running exe.
	args := []string{
		"/c", "timeout /t 1 /nobreak >nul & move /y",
		strconvQuote(staged), strconvQuote(dest), ">nul & start \"\"",
		strconvQuote(dest),
	}
	cmd := exec.Command("cmd", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Start()
	cmd.Process.Release()
	return nil
}

// copyFile copies src to dst preserving contents.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// strconvQuote wraps s in double quotes for a cmd line, escaping embedded
// quotes/percent signs minimally.
func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// ExecutablePath returns the current running binary path.
func ExecutablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}

// Restart re-executes binary with the given args on Windows. The update flow
// on Windows re-launches via the detached stager above, so this is used only
// when no replacement was needed (e.g. already up to date).
func Restart(binary string, args []string) error {
	cmd := exec.Command(binary, args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
