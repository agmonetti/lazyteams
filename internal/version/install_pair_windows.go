//go:build windows

package version

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

func stageWindowsBinary(src string, target updateTarget) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target.path), ".lazyteams-stage-*.exe")
	if err != nil {
		return "", err
	}
	name := out.Name()
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func reserveWindowsPath(dir, pattern string) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func psString(path string) string { return "'" + strings.ReplaceAll(path, "'", "''") + "'" }

func utf16Bytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		bytes[i*2] = byte(unit)
		bytes[i*2+1] = byte(unit >> 8)
	}
	return bytes
}

func installBinaryPair(self updateTarget, tuiSrc string, auth updateTarget, authSrc string) error {
	tuiStage, err := stageWindowsBinary(tuiSrc, self)
	if err != nil {
		return err
	}
	authStage, err := stageWindowsBinary(authSrc, auth)
	if err != nil {
		os.Remove(tuiStage)
		return err
	}
	cleanup := func() { os.Remove(tuiStage); os.Remove(authStage) }
	for _, target := range []updateTarget{self, auth} {
		if err := target.unchanged(); err != nil {
			cleanup()
			return err
		}
	}
	tuiBackup, err := reserveWindowsPath(filepath.Dir(self.path), ".lazyteams-backup-*.exe")
	if err != nil {
		cleanup()
		return err
	}
	authBackup, err := reserveWindowsPath(filepath.Dir(auth.path), ".lazyteams-backup-*.exe")
	if err != nil {
		cleanup()
		return err
	}
	marker, err := reserveWindowsPath(os.TempDir(), "lazyteams-update-result-*.txt")
	if err != nil {
		cleanup()
		return err
	}
	// This script runs outside the locked TUI process. It reports either the
	// completed pair or detailed rollback errors to the uniquely named marker,
	// then launches one TUI with only the result flag (never --update).
	ps := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$self = %s; $auth = %s; $tuiStage = %s; $authStage = %s
$tuiBackup = %s; $authBackup = %s; $marker = %s
$result = 'ok'
try {
  $moved = $false
  for ($i = 0; $i -lt 120; $i++) {
    try { Move-Item -LiteralPath $self -Destination $tuiBackup -ErrorAction Stop; $moved = $true; break }
    catch { Start-Sleep -Milliseconds 500 }
  }
  if (-not $moved) { throw 'running TUI did not exit before update deadline' }
  Move-Item -LiteralPath $auth -Destination $authBackup -ErrorAction Stop
  Move-Item -LiteralPath $tuiStage -Destination $self -ErrorAction Stop
  Move-Item -LiteralPath $authStage -Destination $auth -ErrorAction Stop
} catch {
  $result = 'Paired update failed: ' + $_.ToString()
  try {
    if (Test-Path -LiteralPath $tuiBackup) {
      if (Test-Path -LiteralPath $self) { Remove-Item -LiteralPath $self -Force }
      Move-Item -LiteralPath $tuiBackup -Destination $self -ErrorAction Stop
    }
    if (Test-Path -LiteralPath $authBackup) {
      if (Test-Path -LiteralPath $auth) { Remove-Item -LiteralPath $auth -Force }
      Move-Item -LiteralPath $authBackup -Destination $auth -ErrorAction Stop
    }
  } catch { $result += [Environment]::NewLine + 'Rollback failed: ' + $_.ToString() }
} finally {
  Remove-Item -LiteralPath $tuiStage, $authStage -Force -ErrorAction SilentlyContinue
}
if ($result -eq 'ok') {
  Set-Content -LiteralPath $marker -Value $result -Encoding UTF8
  $started = $false
  for ($i = 0; $i -lt 5; $i++) {
    try {
      Start-Process -FilePath $self -ArgumentList ('"--update-result=' + $marker + '"') -ErrorAction Stop
      $started = $true
      break
    } catch {
      $launchError = $_.ToString()
      Start-Sleep -Seconds 1
    }
  }
  if ($started) {
    Remove-Item -LiteralPath $tuiBackup, $authBackup -Force -ErrorAction SilentlyContinue
    exit
  }
  $result = 'Could not launch new TUI: ' + $launchError
  try {
    if (Test-Path -LiteralPath $tuiBackup) {
      if (Test-Path -LiteralPath $self) { Remove-Item -LiteralPath $self -Force }
      Move-Item -LiteralPath $tuiBackup -Destination $self -ErrorAction Stop
    }
    if (Test-Path -LiteralPath $authBackup) {
      if (Test-Path -LiteralPath $auth) { Remove-Item -LiteralPath $auth -Force }
      Move-Item -LiteralPath $authBackup -Destination $auth -ErrorAction Stop
    }
  } catch { $result += [Environment]::NewLine + 'Rollback failed: ' + $_.ToString() }
}
Set-Content -LiteralPath $marker -Value $result -Encoding UTF8
$launch = $self
if (-not (Test-Path -LiteralPath $launch) -and (Test-Path -LiteralPath $tuiBackup)) { $launch = $tuiBackup }
$started = $false
for ($i = 0; $i -lt 5; $i++) {
  try {
    Start-Process -FilePath $launch -ArgumentList ('"--update-result=' + $marker + '"') -ErrorAction Stop
    $started = $true
    break
  } catch {
    $launchError = $_.ToString()
    Start-Sleep -Seconds 1
  }
}
if (-not $started) {
  $result += [Environment]::NewLine + 'Could not relaunch TUI: ' + $launchError
  Set-Content -LiteralPath $marker -Value $result -Encoding UTF8
  Start-Process -FilePath 'cmd.exe' -ArgumentList ('/K type "' + $marker + '"')
}`, psString(self.path), psString(auth.path), psString(tuiStage), psString(authStage), psString(tuiBackup), psString(authBackup), psString(marker))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(utf16Bytes(ps)))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		cleanup()
		return fmt.Errorf("starting paired update stager: %w", err)
	}
	_ = cmd.Process.Release()
	return ErrUpdatePending
}

// ConsumeUpdateResult reads the outcome after the detached updater restarts
// the TUI. handled distinguishes a normal launch from a transaction result.
func ConsumeUpdateResult(args []string) (handled bool, err error) {
	const prefix = "--update-result="
	var marker string
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			marker = strings.TrimPrefix(arg, prefix)
			break
		}
	}
	if marker == "" {
		return false, nil
	}
	base := filepath.Base(marker)
	random := strings.TrimSuffix(strings.TrimPrefix(base, "lazyteams-update-result-"), ".txt")
	if filepath.Dir(marker) != os.TempDir() || !strings.HasPrefix(base, "lazyteams-update-result-") ||
		!strings.HasSuffix(base, ".txt") || len(random) < 8 || len(random) > 32 ||
		strings.IndexFunc(random, func(ch rune) bool { return ch < '0' || ch > '9' }) >= 0 {
		return true, errors.New("invalid update result marker path")
	}
	fi, err := os.Lstat(marker)
	if err != nil {
		return true, fmt.Errorf("reading update result: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() > 64*1024 {
		return true, errors.New("invalid update result marker")
	}
	if err := ownedByCurrentUser(marker, fi); err != nil {
		return true, err
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return true, err
	}
	if err := os.Remove(marker); err != nil {
		return true, err
	}
	result := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if result != "ok" {
		return true, errors.New(result)
	}
	return true, nil
}
