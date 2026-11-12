//go:build !windows

package version

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func installedTestPair(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	self := filepath.Join(dir, "lazyteams")
	auth := filepath.Join(dir, "lazyteams-auth")
	for _, path := range []string{self, auth} {
		if err := os.WriteFile(path, []byte("old:"+filepath.Base(path)), 0o751); err != nil {
			t.Fatal(err)
		}
	}
	old := checkBinaryIdentity
	checkBinaryIdentity = func(string, string, string) error { return nil }
	t.Cleanup(func() { checkBinaryIdentity = old })
	return self, auth
}

func TestPairedUpdateAndSymlinks(t *testing.T) {
	self, auth := installedTestPair(t)
	dir := t.TempDir()
	selfLink := filepath.Join(dir, "launcher")
	authLink := filepath.Join(dir, "helper")
	if err := os.Symlink(self, selfLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(auth, authLink); err != nil {
		t.Fatal(err)
	}
	tuiName, _ := AssetBaseName(runtime.GOOS, runtime.GOARCH)
	authName, _ := AuthHelperAssetBaseName(runtime.GOOS, runtime.GOARCH)
	contents := map[string]string{tuiName: "new tui", authName: "new helper"}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "latest":
			fmt.Fprintf(w, `{"tag_name":"v999.0.0","assets":[{"name":%q,"browser_download_url":%q},{"name":%q,"browser_download_url":%q},{"name":"SHA256SUMS","browser_download_url":%q}]}`,
				tuiName, srv.URL+"/releases/download/v999.0.0/"+tuiName,
				authName, srv.URL+"/releases/download/v999.0.0/"+authName,
				srv.URL+"/releases/download/v999.0.0/SHA256SUMS")
		case "SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n%s  %s\n", sha256Hex(contents[tuiName]), tuiName, sha256Hex(contents[authName]), authName)
		default:
			if content, ok := contents[filepath.Base(r.URL.Path)]; ok {
				fmt.Fprint(w, content)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	}))
	defer srv.Close()
	info, err := LatestReleaseInfo(srv.URL + "/latest")
	if err != nil {
		t.Fatal(err)
	}
	normalIdentity := checkBinaryIdentity
	checkBinaryIdentity = func(_ string, kind, current string) error {
		if kind == "lazyteams-auth" && current == info.TagName {
			return errors.New("helper belongs to another release")
		}
		return nil
	}
	updated, err := info.UpdateCmd("dev", selfLink, authLink, func(string) bool { return true }, nil)
	if updated || err == nil || !strings.Contains(err.Error(), "another release") {
		t.Fatalf("mislabeled helper update = %v, %v", updated, err)
	}
	for _, path := range []string{self, auth} {
		if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "old:") {
			t.Errorf("invalid release modified %s", path)
		}
	}
	checkBinaryIdentity = normalIdentity
	updated, err = info.UpdateCmd("dev", selfLink, authLink, func(string) bool { return true }, nil)
	if err != nil || !updated {
		t.Fatalf("update = %v, %v", updated, err)
	}
	for path, want := range map[string]string{self: contents[tuiName], auth: contents[authName]} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v, want %q", path, got, err, want)
		}
		mode, err := os.Stat(path)
		if err != nil || mode.Mode().Perm() != 0o751 {
			t.Errorf("%s mode = %v, %v", path, mode, err)
		}
	}
	for _, path := range []string{selfLink, authLink} {
		if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("symlink changed: %s: %v", path, err)
		}
	}
}

func TestPairedUpdateRollsBackSecondSwap(t *testing.T) {
	self, auth := installedTestPair(t)
	dir := t.TempDir()
	tuiSrc, authSrc := filepath.Join(dir, "tui"), filepath.Join(dir, "auth")
	for _, path := range []string{tuiSrc, authSrc} {
		if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := renameInstalledBinary
	renameInstalledBinary = func(src, dst string) error {
		if dst == auth {
			return errors.New("simulated second swap failure")
		}
		return os.Rename(src, dst)
	}
	t.Cleanup(func() { renameInstalledBinary = old })
	selfFi, err := os.Stat(self)
	if err != nil {
		t.Fatal(err)
	}
	authFi, err := os.Stat(auth)
	if err != nil {
		t.Fatal(err)
	}
	err = installBinaryPair(updateTarget{path: self, mode: 0o751, original: selfFi}, tuiSrc, updateTarget{path: auth, mode: 0o751, original: authFi}, authSrc)
	if err == nil || !strings.Contains(err.Error(), "simulated second swap failure") {
		t.Fatalf("installBinaryPair error = %v", err)
	}
	for path, want := range map[string]string{self: "old:lazyteams", auth: "old:lazyteams-auth"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v, want %q", path, got, err, want)
		}
	}
}

func TestPairedUpdateRefusesUnrecognizedDestination(t *testing.T) {
	self, auth := installedTestPair(t)
	unowned := filepath.Join(filepath.Dir(auth), "private-user-data")
	if err := os.WriteFile(unowned, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := ReleaseInfo{TagName: "v999.0.0", hasChecksumAsset: true, assetURLs: map[string]string{}, digestByName: map[string]string{}}
	tuiName, _ := AssetBaseName(runtime.GOOS, runtime.GOARCH)
	authName, _ := AuthHelperAssetBaseName(runtime.GOOS, runtime.GOARCH)
	for _, name := range []string{tuiName, authName} {
		info.assetURLs[name] = "https://github.com/example/release/" + name
		info.digestByName[name] = sha256Hex("new")
	}
	if updated, err := info.UpdateCmd("dev", self, unowned, nil, nil); updated || err == nil {
		t.Fatalf("unsafe destination: updated=%v err=%v", updated, err)
	}
	if got, _ := os.ReadFile(unowned); string(got) != "keep" {
		t.Errorf("user file was changed: %q", got)
	}
	if updated, err := info.UpdateCmd("v999.0.0", self, auth, nil, nil); updated || err != nil {
		t.Errorf("same release should not update: updated=%v err=%v", updated, err)
	}
	delete(info.assetURLs, authName)
	if updated, err := info.UpdateCmd("dev", self, auth, nil, nil); updated || err == nil {
		t.Errorf("missing helper asset must fail: updated=%v err=%v", updated, err)
	}
}

func TestPreflightRejectsUnrecognizedBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazyteams")
	if err := os.WriteFile(path, []byte("not a Go binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareUpdateTarget(path, "lazyteams", "dev"); err == nil || !strings.Contains(err.Error(), "not a recognizable lazyteams build") {
		t.Fatalf("preflight must reject unrelated executable: %v", err)
	}
}

func TestResolveAuthHelperPathResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "lazyteams")
	helper := filepath.Join(dir, "lazyteams-auth")
	target := filepath.Join(t.TempDir(), "real-helper")
	if err := os.WriteFile(target, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, helper); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveAuthHelperPath(self)
	if err != nil || got != target {
		t.Fatalf("resolved auth helper = %q, %v, want %q", got, err, target)
	}
}
