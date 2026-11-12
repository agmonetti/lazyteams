package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrUpdatePending means a Windows stager has taken responsibility for the
// verified pair. The caller must exit promptly; completion is reported on
// restart through ConsumeUpdateResult, not at staging time.
var ErrUpdatePending = errors.New("paired update staged; exit to complete installation")

// allowedDownloadHost checks that a download/redirect target is a GitHub host
// over HTTPS. The self-updater only ever fetches release assets from GitHub,
// which may redirect to release-assets.githubusercontent.com or
// objects.githubusercontent.com. Anything else is treated as untrusted.
func allowedDownloadHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "github.com",
		"release-assets.githubusercontent.com",
		"objects.githubusercontent.com",
		"api.github.com":
		return true
	}
	return false
}

// downloadClient is used for binary downloads (and the SHA256SUMS file). It
// has a longer timeout than the release-check client because a release binary
// can be tens of megabytes, and it validates every hop stays on GitHub hosts.
var downloadClient = &http.Client{
	Timeout: 5 * time.Minute,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if !allowedDownloadHost(req.URL.String()) {
			return fmt.Errorf("refusing redirect to non-GitHub host %q", req.URL.Host)
		}
		return nil
	},
}

// AssetBaseName returns the GitHub release asset filename for a given
// GOOS/GOARCH pair. It mirrors the naming convention used by the Release
// workflow: lazyteams-<os>-<arch>[.exe]. Unsupported combinations return an
// error so the updater fails with a clear message instead of guessing.
func AssetBaseName(goos, goarch string) (string, error) {
	var ext string
	if goos == "windows" {
		ext = ".exe"
	}
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return "", fmt.Errorf("unsupported OS %q", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported architecture %q for %s", goarch, goos)
	}
	return "lazyteams-" + goos + "-" + goarch + ext, nil
}

// AuthHelperAssetBaseName returns the auth-helper asset filename for a given
// GOOS/GOARCH pair, following the same convention with the lazyteams-auth
// prefix.
func AuthHelperAssetBaseName(goos, goarch string) (string, error) {
	name, err := AssetBaseName(goos, goarch)
	if err != nil {
		return "", err
	}
	return strings.Replace(name, "lazyteams-", "lazyteams-auth-", 1), nil
}

// ReleaseInfo contains the assets and published checksums from one release.
type ReleaseInfo struct {
	TagName          string
	DownloadBase     string
	digestByName     map[string]string
	assetURLs        map[string]string
	hasChecksumAsset bool
}

// ReleaseAsset describes a single release asset returned by the GitHub API.
type ReleaseAsset struct {
	Name            string `json:"name"`
	BrowserDownload string `json:"browser_download_url"`
	Size            int64  `json:"size"`
}

// Digests returns a copy of the asset digest map used to verify downloads.
func (r ReleaseInfo) Digests() map[string]string {
	out := make(map[string]string, len(r.digestByName))
	for k, v := range r.digestByName {
		out[k] = v
	}
	return out
}

// LatestReleaseInfo obtains one release and its published SHA256SUMS.
// Binary downloads are restricted to assets explicitly listed in that release.
func LatestReleaseInfo(apiURL string) (ReleaseInfo, error) {
	body, err := fetchLatestReleaseJSON(apiURL)
	if err != nil {
		return ReleaseInfo{}, err
	}

	var payload struct {
		TagName    string         `json:"tag_name"`
		Assets     []ReleaseAsset `json:"assets"`
		ZipballURL string         `json:"zipball_url"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ReleaseInfo{}, err
	}
	if payload.TagName == "" {
		return ReleaseInfo{}, errors.New("github releases: empty tag_name")
	}

	info := ReleaseInfo{
		TagName:      payload.TagName,
		digestByName: map[string]string{},
		assetURLs:    map[string]string{},
	}

	// Every asset URL must belong to this exact release. Do not synthesize
	// URLs: an absent binary in the API response must not be installed.
	api, err := url.Parse(apiURL)
	if err != nil {
		return ReleaseInfo{}, err
	}
	suffix := "/releases/download/" + url.PathEscape(payload.TagName) + "/"
	for _, a := range payload.Assets {
		u, err := url.Parse(a.BrowserDownload)
		if err != nil || a.Name == "" || u.RawQuery != "" || u.Fragment != "" ||
			!strings.HasSuffix(u.EscapedPath(), suffix+url.PathEscape(a.Name)) {
			return ReleaseInfo{}, fmt.Errorf("invalid release asset URL for %q", a.Name)
		}
		if api.Hostname() == "api.github.com" {
			repo := strings.TrimPrefix(strings.TrimSuffix(api.EscapedPath(), "/releases/latest"), "/repos/")
			if u.Scheme != "https" || u.Hostname() != "github.com" ||
				u.EscapedPath() != "/"+repo+suffix+url.PathEscape(a.Name) {
				return ReleaseInfo{}, fmt.Errorf("release asset %q is not from the queried GitHub release", a.Name)
			}
		} else if u.Scheme != api.Scheme || u.Host != api.Host {
			return ReleaseInfo{}, fmt.Errorf("release asset %q is not from the queried release host", a.Name)
		}
		base := strings.TrimSuffix(a.BrowserDownload, "/"+url.PathEscape(a.Name))
		if info.DownloadBase != "" && base != info.DownloadBase {
			return ReleaseInfo{}, fmt.Errorf("release asset %q belongs to another release", a.Name)
		}
		info.DownloadBase = base
		if _, exists := info.assetURLs[a.Name]; exists {
			return ReleaseInfo{}, fmt.Errorf("duplicate release asset %q", a.Name)
		}
		info.assetURLs[a.Name] = a.BrowserDownload
	}

	// Parse SHA256SUMS if it's present as an asset.
	for _, a := range payload.Assets {
		if a.Name == "SHA256SUMS" {
			sums, err := downloadSHA256SUMS(a.BrowserDownload)
			if err != nil {
				return ReleaseInfo{}, fmt.Errorf("reading SHA256SUMS: %w", err)
			}
			info.digestByName = sums
			info.hasChecksumAsset = true
			break
		}
	}
	return info, nil
}

// DownloadSHA256SUMS fetches and parses a SHA256SUMS (hash <space> filename)
// into a map keyed by lowercased filename.
func downloadSHA256SUMS(url string) (map[string]string, error) {
	if url == "" {
		return nil, errors.New("empty SHA256SUMS url")
	}
	resp, err := downloadClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SHA256SUMS status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sums[filepath.Base(fields[1])] = strings.ToLower(fields[0])
	}
	return sums, nil
}

// DownloadFile streams url to dest (writing to a temporary file first, which
// is renamed over dest on success). progress, if non-nil, receives a one-line
// human-readable progress string that gets overwritten each call. The caller
// is responsible for any integrity verification on the returned file.
func DownloadFile(url, dest string, progress func(string)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "lazyteams")
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".lazyteams-update-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				tmp.Close()
				return werr
			}
			written += int64(n)
			if progress != nil && total > 0 {
				pct := float64(written) / float64(total) * 100
				progress(fmt.Sprintf("\rDownloading %s: %.1f%% (%.1f/%.1f MB)",
					filepath.Base(dest), pct, float64(written)/1e6, float64(total)/1e6))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return rerr
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if progress != nil && total > 0 {
		progress("\n")
	}
	return os.Rename(tmp.Name(), dest)
}

// VerifySHA256 reports whether file's SHA-256 hex digest equals expectHex
// (compared case-insensitively). It is used to validate a downloaded binary
// against a published checksum before replacing the running binary.
func VerifySHA256(file, expectHex string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(expectHex)) {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, strings.TrimSpace(expectHex))
	}
	return nil
}

// VerifyFileSHA256 hashes file and compares it against the digest for name
// published in release. Returns a helpful error if the release did not
// publish a checksum for the asset.
func (r ReleaseInfo) VerifyFileSHA256(file, name string) error {
	digest, ok := r.digestByName[filepath.Base(name)]
	if !ok {
		return fmt.Errorf("no published SHA-256 checksum for %s", filepath.Base(name))
	}
	return VerifySHA256(file, digest)
}

// AuthHelperSiblingPath returns the conventional helper location next to selfExe.
func AuthHelperSiblingPath(selfExe string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(filepath.Dir(selfExe), "lazyteams-auth"+ext)
}

// ResolveAuthHelperPath locates the helper the UI actually launches: next to
// the raw executable path, which can differ from the symlink-resolved TUI.
// It never invents a new installation location.
func ResolveAuthHelperPath(selfExe string) (string, error) {
	raw, err := os.Executable()
	if err != nil {
		return "", err
	}
	for _, candidate := range []string{AuthHelperSiblingPath(raw), AuthHelperSiblingPath(selfExe)} {
		fi, err := os.Stat(candidate)
		if err == nil {
			if !fi.Mode().IsRegular() || (runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0) {
				return "", fmt.Errorf("auth helper %s is not executable", candidate)
			}
			return filepath.EvalSymlinks(candidate)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("locating auth helper %s: %w", candidate, err)
		}
	}
	return "", errors.New("lazyteams-auth is not installed alongside the launcher or TUI")
}

// UpdateCmd installs both verified assets from the same newer release, or
// leaves both existing binaries untouched on failure.
func (r ReleaseInfo) UpdateCmd(current, selfExe, authExe string, confirm func(string) bool, progress func(string)) (bool, error) {
	if Compare(current, r.TagName) >= 0 {
		return false, nil
	}
	tuiName, err := AssetBaseName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}
	authName, err := AuthHelperAssetBaseName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}
	if !r.hasChecksumAsset {
		return false, errors.New("release does not publish SHA256SUMS; refusing to self-update")
	}
	for _, name := range []string{tuiName, authName} {
		if r.assetURLs[name] == "" || r.digestByName[name] == "" {
			return false, fmt.Errorf("release %s lacks asset or SHA-256 checksum for %s", r.TagName, name)
		}
	}
	// Check ownership, identity, symlinks and both writable directories before
	// requesting confirmation or downloading anything.
	self, err := prepareUpdateTarget(selfExe, "lazyteams", current)
	if err != nil {
		return false, err
	}
	auth, err := prepareUpdateTarget(authExe, "lazyteams-auth", current)
	if err != nil {
		return false, err
	}
	if self.path == auth.path {
		return false, errors.New("TUI and auth helper resolve to the same file")
	}
	if confirm != nil && !confirm(fmt.Sprintf("Release %s available. Update lazyteams and lazyteams-auth?", r.TagName)) {
		if progress != nil {
			progress("Update cancelled.\n")
		}
		return false, nil
	}
	tuiTmp, err := os.CreateTemp("", "lazyteams-new-*")
	if err != nil {
		return false, err
	}
	tuiTmp.Close()
	defer os.Remove(tuiTmp.Name())
	authTmp, err := os.CreateTemp("", "lazyteams-auth-new-*")
	if err != nil {
		return false, err
	}
	authTmp.Close()
	defer os.Remove(authTmp.Name())

	for _, asset := range []struct{ name, path, kind string }{
		{tuiName, tuiTmp.Name(), "lazyteams"},
		{authName, authTmp.Name(), "lazyteams-auth"},
	} {
		if progress != nil {
			progress(fmt.Sprintf("Downloading %s...\n", asset.name))
		}
		if err := DownloadFile(r.assetURLs[asset.name], asset.path, nil); err != nil {
			return false, err
		}
		if err := r.VerifyFileSHA256(asset.path, asset.name); err != nil {
			return false, err
		}
		if err := checkBinaryIdentity(asset.path, asset.kind, r.TagName); err != nil {
			return false, fmt.Errorf("release %s contains an invalid %s build: %w", r.TagName, asset.kind, err)
		}
	}
	if progress != nil {
		progress("Installing...\n")
	}
	if err := installBinaryPair(self, tuiTmp.Name(), auth, authTmp.Name()); err != nil {
		return false, err
	}
	if progress != nil {
		progress(fmt.Sprintf("Updated to %s.\n", r.TagName))
	}
	return true, nil
}
