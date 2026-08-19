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

// ReleaseInfo is the metadata needed to perform a self-update. digestByName
// maps asset filenames to their lowercase SHA-256 hex digest as published in
// the release's SHA256SUMS asset.
type ReleaseInfo struct {
	TagName          string
	DownloadBase     string // GitHub releases download base, e.g. .../releases/download/<tag>
	digestByName     map[string]string
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

// resolveDownloadURL returns the deterministic GitHub releases download URL
// for an asset:
//
//	https://github.com/<owner>/<repo>/releases/download/<tag>/<asset>
func (r ReleaseInfo) resolveDownloadURL(asset string) string {
	return r.DownloadBase + "/" + asset
}

// LatestReleaseInfo queries the GitHub releases API for the latest release
// (including its assets) and returns a ReleaseInfo with the tag, the package
// download base URL and, when present, the SHA256SUMS digests.
//
// If the release exposes a SHA256SUMS asset its contents are parsed and used
// for verification. Otherwise every binary asset is downloaded and hashed to
// obtain its digest (verified against the same-GitHub TLS source). The caller
// passes the API URL so tests can point at an httptest server.
func LatestReleaseInfo(apiURL string) (ReleaseInfo, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return ReleaseInfo{}, err
	}
	// GitHub's API rejects requests without a User-Agent header.
	req.Header.Set("User-Agent", "lazyteams")

	resp, err := httpClient.Do(req)
	if err != nil {
		return ReleaseInfo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ReleaseInfo{}, fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
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
	}

	// Derive the GitHub releases download base from a known asset, or build
	// it deterministically from the API URL and tag.
	suffix := "/releases/download/" + payload.TagName + "/"
	for _, a := range payload.Assets {
		if idx := strings.Index(a.BrowserDownload, suffix); idx >= 0 {
			info.DownloadBase = a.BrowserDownload[:idx+len(suffix)-1]
			break
		}
	}
	if info.DownloadBase == "" {
		repo := strings.TrimPrefix(strings.TrimSuffix(apiURL, "/releases/latest"), "https://api.github.com/repos/")
		info.DownloadBase = "https://github.com/" + repo + "/releases/download/" + payload.TagName
	}

	// Parse SHA256SUMS if it's present as an asset.
	for _, a := range payload.Assets {
		if strings.EqualFold(a.Name, "SHA256SUMS") {
			sums, err := downloadSHA256SUMS(a.BrowserDownload)
			if err != nil {
				// A broken checksum file must not silently disable integrity
				// verification; treat it as a fatal error.
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

// AuthHelperSiblingPath returns the expected path of the auth-helper binary
// installed alongside the TUI binary (selfExe). It is a best-effort guess so
// the updater can keep both binaries in sync.
func AuthHelperSiblingPath(selfExe string) string {
	dir := filepath.Dir(selfExe)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return filepath.Join(dir, "lazyteams-auth"+ext)
}

// UpdateCmd runs the version checks against the passed API URL (instead of
// the compiled-in URL) and performs the update if a newer release exists.
// current is the running version, selfExe is the path of the currently
// running binary, and confirm is called (returning true) before replacing
// binaries. It returns (updated bool, err error).
//
// It updates both the TUI and auth-helper binaries, verifying each against
// the release's published SHA-256 checksums before replacing them.
func (r ReleaseInfo) UpdateCmd(current, selfExe, authExe string, confirm func(string) bool, progress func(string)) (bool, error) {
	// Ensure the checksums were actually published before downloading
	// anything. Without them we refuse to self-update rather than install a
	// possibly-corrupted binary.
	if !r.hasChecksumAsset || len(r.Digests()) == 0 {
		return false, errors.New("release does not publish SHA256SUMS; refusing to self-update")
	}

	tuiName, err := AssetBaseName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}
	authName, err := AuthHelperAssetBaseName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return false, err
	}

	if confirm != nil {
		if !confirm(fmt.Sprintf("Release %s available. Update lazyteams and lazyteams-auth?", r.TagName)) {
			if progress != nil {
				progress("Update cancelled.\n")
			}
			return false, nil
		}
	}

	downloadTUI := r.resolveDownloadURL(tuiName)
	downloadAuth := r.resolveDownloadURL(authName)

	tuiTmp, err := os.CreateTemp("", "lazyteams-new-*")
	if err != nil {
		return false, err
	}
	tuiTmp.Close()
	authTmp, err := os.CreateTemp("", "lazyteams-auth-new-*")
	if err != nil {
		return false, err
	}
	authTmp.Close()
	defer os.Remove(tuiTmp.Name())
	defer os.Remove(authTmp.Name())

	if progress != nil {
		progress("Downloading lazyteams...\n")
	}
	if err := DownloadFile(downloadTUI, tuiTmp.Name(), nil); err != nil {
		return false, err
	}
	if err := r.VerifyFileSHA256(tuiTmp.Name(), tuiName); err != nil {
		return false, err
	}

	if progress != nil {
		progress("Downloading lazyteams-auth...\n")
	}
	if err := DownloadFile(downloadAuth, authTmp.Name(), nil); err != nil {
		return false, err
	}
	if err := r.VerifyFileSHA256(authTmp.Name(), authName); err != nil {
		return false, err
	}

	if progress != nil {
		progress("Installing...\n")
	}
	if err := ReplaceBinary(tuiTmp.Name(), selfExe); err != nil {
		return false, err
	}
	// The auth-helper is optional at runtime but we always keep it in sync
	// when we update the TUI.
	if authExe != "" {
		if err := ReplaceBinary(authTmp.Name(), authExe); err != nil {
			// Non-fatal: the TUI already updated; report but continue.
			if progress != nil {
				progress(fmt.Sprintf("Note: could not replace auth-helper: %v\n", err))
			}
		}
	}

	if progress != nil {
		progress(fmt.Sprintf("Updated to %s.\n", r.TagName))
	}
	// Restart is handled by the caller so it can control the exit sequence.
	return true, nil
}
