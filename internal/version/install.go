package version

import (
	"debug/buildinfo"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const modulePath = "github.com/agmonetti/lazyteams"

type updateTarget struct {
	path     string
	mode     os.FileMode
	original os.FileInfo
}

func (target updateTarget) unchanged() error {
	fi, err := os.Lstat(target.path)
	if err != nil {
		return err
	}
	if target.original == nil || !os.SameFile(target.original, fi) {
		return fmt.Errorf("installed binary %s changed during download; refusing to overwrite it", target.path)
	}
	return nil
}

var checkBinaryIdentity = verifyBinaryIdentity

// verifyBinaryIdentity rejects unrelated executables even if their filename
// happens to match. Release builds must also contain the injected version.
func verifyBinaryIdentity(path, kind, current string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return fmt.Errorf("not a recognizable lazyteams build: %w", err)
	}
	wantPath := modulePath
	if kind == "lazyteams-auth" {
		wantPath += "/cmd/auth-helper"
	}
	if info.Path != wantPath || info.Main.Path != modulePath {
		return fmt.Errorf("build identity %q / %q does not match %s", info.Path, info.Main.Path, wantPath)
	}
	want := modulePath + "/internal/version.Version=" + current
	var injected bool
	for _, setting := range info.Settings {
		if setting.Key != "-ldflags" {
			continue
		}
		for _, flag := range strings.Fields(setting.Value) {
			if flag == want {
				injected = true
			} else if strings.HasPrefix(flag, modulePath+"/internal/version.Version=") {
				return fmt.Errorf("%s has a different injected build version", path)
			}
		}
	}
	if current != "dev" && !injected {
		return fmt.Errorf("%s has no verifiable build version %s", path, current)
	}
	return nil
}

func prepareUpdateTarget(path, kind, current string) (updateTarget, error) {
	if path == "" {
		return updateTarget{}, fmt.Errorf("missing installed %s path", kind)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return updateTarget{}, fmt.Errorf("resolving %s: %w", path, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return updateTarget{}, err
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	base := filepath.Base(resolved)
	asset, err := AssetBaseName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return updateTarget{}, err
	}
	if kind == "lazyteams-auth" {
		asset, err = AuthHelperAssetBaseName(runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return updateTarget{}, err
		}
	}
	if base != kind+ext && base != asset {
		return updateTarget{}, fmt.Errorf("refusing unrecognized %s destination %s", kind, resolved)
	}
	fi, err := os.Lstat(resolved)
	if err != nil {
		return updateTarget{}, err
	}
	if !fi.Mode().IsRegular() {
		return updateTarget{}, fmt.Errorf("refusing non-regular %s destination %s", kind, resolved)
	}
	if err := ownedByCurrentUser(resolved, fi); err != nil {
		return updateTarget{}, err
	}
	if err := checkBinaryIdentity(resolved, kind, current); err != nil {
		return updateTarget{}, fmt.Errorf("refusing unowned %s destination: %w", kind, err)
	}
	// Creating a temporary file in the destination directory verifies we can
	// actually perform the adjacent-file swap before any download occurs.
	probe, err := os.CreateTemp(filepath.Dir(resolved), ".lazyteams-preflight-*")
	if err != nil {
		return updateTarget{}, fmt.Errorf("cannot write beside %s: %w", resolved, err)
	}
	if err := probe.Close(); err != nil {
		os.Remove(probe.Name())
		return updateTarget{}, err
	}
	if err := os.Remove(probe.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return updateTarget{}, err
	}
	return updateTarget{path: resolved, mode: fi.Mode().Perm(), original: fi}, nil
}
