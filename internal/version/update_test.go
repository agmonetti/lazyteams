package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetBaseName(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{"linux", "amd64", "lazyteams-linux-amd64", false},
		{"linux", "arm64", "lazyteams-linux-arm64", false},
		{"darwin", "amd64", "lazyteams-darwin-amd64", false},
		{"darwin", "arm64", "lazyteams-darwin-arm64", false},
		{"windows", "amd64", "lazyteams-windows-amd64.exe", false},
		{"windows", "arm64", "lazyteams-windows-arm64.exe", false},
		{"freebsd", "amd64", "", true},
		{"linux", "386", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			got, err := AssetBaseName(tt.goos, tt.goarch)
			if tt.wantErr {
				if err == nil {
					t.Errorf("AssetBaseName(%q,%q) expected error, got %q", tt.goos, tt.goarch, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("AssetBaseName(%q,%q) unexpected error: %v", tt.goos, tt.goarch, err)
			}
			if got != tt.want {
				t.Errorf("AssetBaseName(%q,%q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestAuthHelperAssetBaseName(t *testing.T) {
	got, err := AuthHelperAssetBaseName("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got != "lazyteams-auth-linux-amd64" {
		t.Errorf("got %q", got)
	}
	got, _ = AuthHelperAssetBaseName("windows", "amd64")
	if got != "lazyteams-auth-windows-amd64.exe" {
		t.Errorf("got %q", got)
	}
}

// TestLatestReleaseInfo verifies the release API parsing: tag, download base
// derivation and SHA256SUMS parsing.
func TestLatestReleaseInfo(t *testing.T) {
	tuiName := "lazyteams-linux-amd64"
	authName := "lazyteams-auth-linux-amd64"

	// Build SHA256SUMS content for the two binaries.
	var sb strings.Builder
	h := sha256.Sum256([]byte("binary-a"))
	fmt.Fprintf(&sb, "%x  %s\n", h, tuiName)
	h = sha256.Sum256([]byte("binary-b"))
	fmt.Fprintf(&sb, "%x  %s\n", h, authName)
	sumsContent := sb.String()

	const tag = "v2.0.0"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case "sums":
			fmt.Fprint(w, sumsContent)
		case tuiName:
			fmt.Fprint(w, "binary-a")
		case authName:
			fmt.Fprint(w, "binary-b")
		case "latest":
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[`+
				`{"name":%q,"browser_download_url":%q},`+
				`{"name":%q,"browser_download_url":%q},`+
				`{"name":"SHA256SUMS","browser_download_url":%q}]}`,
				tag,
				tuiName, srv.URL+"/releases/download/"+tag+"/"+tuiName,
				authName, srv.URL+"/releases/download/"+tag+"/"+authName,
				srv.URL+"/releases/download/"+tag+"/sums",
			)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	info, err := LatestReleaseInfo(srv.URL + "/latest")
	if err != nil {
		t.Fatal(err)
	}
	if info.TagName != tag {
		t.Errorf("TagName = %q, want %q", info.TagName, tag)
	}
	if !info.hasChecksumAsset {
		t.Error("hasChecksumAsset should be true")
	}
	wantBase := srv.URL + "/releases/download/" + tag
	if info.DownloadBase != wantBase {
		t.Errorf("DownloadBase = %q, want %q", info.DownloadBase, wantBase)
	}
	wantDigest := sha256Hex("binary-a")
	if got := info.Digests()[tuiName]; got != wantDigest {
		t.Errorf("digest for %s = %q, want %q", tuiName, got, wantDigest)
	}
}

// sha256Hex is a small helper returning the hex SHA-256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestLatestReleaseInfoNoChecksums(t *testing.T) {
	const tag = "v2.0.0"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "latest" {
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":"lazyteams-linux-amd64","browser_download_url":%q}]}`,
				tag, srv.URL+"/releases/download/"+tag+"/lazyteams-linux-amd64")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	info, err := LatestReleaseInfo(srv.URL + "/latest")
	if err != nil {
		t.Fatal(err)
	}
	if info.hasChecksumAsset {
		t.Error("should not have checksum asset when none published")
	}
	if len(info.Digests()) != 0 {
		t.Errorf("expected no digests, got %v", info.Digests())
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bin")
	content := []byte("test binary content")
	if err := os.WriteFile(file, content, 0o755); err != nil {
		t.Fatal(err)
	}
	good := sha256Hex(string(content))
	if err := VerifySHA256(file, good); err != nil {
		t.Errorf("VerifySHA256 with correct digest: %v", err)
	}
	if err := VerifySHA256(file, strings.Repeat("0", 64)); err == nil {
		t.Error("VerifySHA256 should fail on wrong digest")
	}
	if err := VerifySHA256(filepath.Join(dir, "missing"), good); err == nil {
		t.Error("VerifySHA256 should fail on missing file")
	}
}

func TestDownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello world")
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	if err := DownloadFile(srv.URL, dest, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "hello world" {
		t.Errorf("downloaded %q, want %q", data, "hello world")
	}
}

func TestDownloadFileHttpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := DownloadFile(srv.URL, filepath.Join(dir, "x"), nil); err == nil {
		t.Error("expected error on 404")
	}
}

func TestVerifyNoPublishedChecksum(t *testing.T) {
	info := ReleaseInfo{hasChecksumAsset: false, digestByName: map[string]string{}}
	dir := t.TempDir()
	file := filepath.Join(dir, "bin")
	os.WriteFile(file, []byte("x"), 0o755)
	if err := info.VerifyFileSHA256(file, "lazyteams-linux-amd64"); err == nil {
		t.Error("expected error when no published checksum")
	}
}

func TestResolverUsesDownloadBase(t *testing.T) {
	info := ReleaseInfo{TagName: "v1.0.0", DownloadBase: "https://github.com/o/r/releases/download/v1.0.0"}
	got := info.resolveDownloadURL("lazyteams-linux-amd64")
	want := "https://github.com/o/r/releases/download/v1.0.0/lazyteams-linux-amd64"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestAllowedDownloadHost(t *testing.T) {
	allowed := []string{
		"https://github.com/agmonetti/lazyteams/releases/download/v1.0.0/lazyteams-linux-amd64",
		"https://release-assets.githubusercontent.com/abc/def?token=x",
		"https://objects.githubusercontent.com/github-production-release-asset/xyz",
		"https://api.github.com/repos/o/r",
	}
	denied := []string{
		"http://github.com/lazyteams/lazyteams",                     // not https
		"https://evil.example.com/releases/download/x/a",            // non-GitHub host
		"https://release-assets.githubusercontent.com.evil.com/foo", // subdomain trick not in allowlist
		"not-a-url",
		"ftp://github.com/foo",
	}
	for _, u := range allowed {
		if !allowedDownloadHost(u) {
			t.Errorf("allowedDownloadHost(%q) = false, want true", u)
		}
	}
	for _, u := range denied {
		if allowedDownloadHost(u) {
			t.Errorf("allowedDownloadHost(%q) = true, want false", u)
		}
	}
}
