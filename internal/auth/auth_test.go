package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// jwtToken builds a realistic JWT whose payload is base64url-encoded (no
// padding), matching how real Microsoft tokens arrive. Only the payload
// matters for ParseUserNameFromToken.
func jwtToken(claims map[string]interface{}) string {
	b, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func TestParseUserNameFromToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"name claim", jwtToken(map[string]interface{}{"name": "Alice Johnson"}), "Alice Johnson"},
		{"unique_name claim", jwtToken(map[string]interface{}{"unique_name": "alice@contoso.com"}), "alice@contoso.com"},
		{"preferred_username claim", jwtToken(map[string]interface{}{"preferred_username": "alice@contoso.com"}), "alice@contoso.com"},
		{"upn claim", jwtToken(map[string]interface{}{"upn": "alice@contoso.com"}), "alice@contoso.com"},
		{"given_name claim", jwtToken(map[string]interface{}{"given_name": "Alice"}), "Alice"},
		{"name wins over given_name", jwtToken(map[string]interface{}{"name": "Alice Johnson", "given_name": "Alice"}), "Alice Johnson"},
		{"name wins over unique_name", jwtToken(map[string]interface{}{"name": "N", "unique_name": "u@x.com"}), "N"},
		{"empty name falls through", jwtToken(map[string]interface{}{"name": "", "given_name": "Alice"}), "Alice"},
		{"non-string name falls through", jwtToken(map[string]interface{}{"name": 123, "given_name": "Alice"}), "Alice"},
		{"no known claim", jwtToken(map[string]interface{}{"foo": "bar"}), ""},
		{"no dots", "notajwt", ""},
		{"two parts short payload", "a.b", ""},
		{"invalid base64 payload", "a.###.c", ""},
		{"valid base64 not json", "a." + base64.RawURLEncoding.EncodeToString([]byte("hello")) + ".c", ""},
		{"json array payload", "a." + base64.RawURLEncoding.EncodeToString([]byte("[]")) + ".c", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseUserNameFromToken(tt.token); got != tt.want {
				t.Errorf("ParseUserNameFromToken(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

// isolateConfigDir points ConfigDir at a fresh temp dir by setting both home
// variables os.UserHomeDir resolves on Unix ($HOME) and Windows (%USERPROFILE%).
// Returns the fake config dir so tests can write tokens.env into it.
func isolateConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return filepath.Join(dir, ".config", "lazyteams")
}

func writeTokensFile(t *testing.T, configDir, content string) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tokens.env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTokensFile(t *testing.T) {
	t.Run("missing file returns empty map", func(t *testing.T) {
		isolateConfigDir(t)
		if got := loadTokensFile(); len(got) != 0 {
			t.Errorf("loadTokensFile() = %v, want empty map", got)
		}
	})

	t.Run("parses export, quotes and skips invalid lines", func(t *testing.T) {
		configDir := isolateConfigDir(t)
		writeTokensFile(t, configDir, `# comment line
MS_GRAPH_TOKEN=abc123
export TEAMS_WEB_TOKEN = "def 456"
TEAMS_COOKIE = quoted
invalid line without equals
LAST=value
`)
		got := loadTokensFile()
		want := map[string]string{
			"MS_GRAPH_TOKEN":  "abc123",
			"TEAMS_WEB_TOKEN": "def 456",
			"TEAMS_COOKIE":    "quoted",
			"LAST":            "value",
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("loadTokensFile()[%q] = %q, want %q", k, got[k], v)
			}
		}
		if len(got) != len(want) {
			t.Errorf("loadTokensFile() has %d keys, want %d: %v", len(got), len(want), got)
		}
	})

	t.Run("unquotes escaped values written by auth helper", func(t *testing.T) {
		configDir := isolateConfigDir(t)
		writeTokensFile(t, configDir, "TEAMS_COOKIE=\"line1\\nline2\\\"quoted\\\"\\\\slash\"\n")
		got := loadTokensFile()
		want := "line1\nline2\"quoted\"\\slash"
		if got["TEAMS_COOKIE"] != want {
			t.Errorf("loadTokensFile()[%q] = %q, want %q", "TEAMS_COOKIE", got["TEAMS_COOKIE"], want)
		}
	})
}

func TestGetTokens(t *testing.T) {
	allKeys := []string{
		"MS_GRAPH_TOKEN", "TEAMS_WEB_TOKEN", "TEAMS_NOTIF_TOKEN", "EDU_TOKEN",
		"TEAMS_COOKIE", "EDU_COOKIE", "TEAMS_SPACES_TOKEN", "TEAMS_FABRIC_TOKEN",
	}
	clearEnv := func(t *testing.T) {
		t.Helper()
		for _, k := range allKeys {
			t.Setenv(k, "")
		}
	}

	t.Run("reads all tokens from env and strips Bearer", func(t *testing.T) {
		isolateConfigDir(t)
		values := map[string]string{
			"MS_GRAPH_TOKEN":     "Bearer graph",
			"TEAMS_WEB_TOKEN":    "Bearer web",
			"TEAMS_NOTIF_TOKEN":  "notif",
			"EDU_TOKEN":          "Bearer edu",
			"TEAMS_COOKIE":       "cookie",
			"EDU_COOKIE":         " edu-cookie ",
			"TEAMS_SPACES_TOKEN": "Bearer spaces",
			"TEAMS_FABRIC_TOKEN": "fabric",
		}
		for k, v := range values {
			t.Setenv(k, v)
		}

		graph, web, notif, edu, cookie, eduCookie, spaces, fabric, err := GetTokens()
		if err != nil {
			t.Fatalf("GetTokens() error: %v", err)
		}
		got := []string{graph, web, notif, edu, cookie, eduCookie, spaces, fabric}
		want := []string{"graph", "web", "notif", "edu", "cookie", "edu-cookie", "spaces", "fabric"}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("GetTokens()[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("errors when graph or web missing", func(t *testing.T) {
		isolateConfigDir(t)
		clearEnv(t)
		if _, _, _, _, _, _, _, _, err := GetTokens(); err == nil {
			t.Fatal("GetTokens() expected error when graph/web tokens are missing")
		} else if !strings.Contains(err.Error(), "Tokens not found") {
			t.Errorf("GetTokens() error = %q, want it to mention missing tokens", err)
		}
	})

	t.Run("errors when cookie missing", func(t *testing.T) {
		isolateConfigDir(t)
		clearEnv(t)
		t.Setenv("MS_GRAPH_TOKEN", "graph")
		t.Setenv("TEAMS_WEB_TOKEN", "web")
		if _, _, _, _, _, _, _, _, err := GetTokens(); err == nil {
			t.Fatal("GetTokens() expected error when TEAMS_COOKIE is missing")
		} else if !strings.Contains(err.Error(), "Missing TEAMS_COOKIE") {
			t.Errorf("GetTokens() error = %q, want it to mention the cookie", err)
		}
	})

	t.Run("falls back to tokens file when env is empty", func(t *testing.T) {
		configDir := isolateConfigDir(t)
		writeTokensFile(t, configDir, "MS_GRAPH_TOKEN=file-graph\nTEAMS_WEB_TOKEN=file-web\nTEAMS_COOKIE=file-cookie\n")
		clearEnv(t)

		graph, web, _, _, cookie, _, _, _, err := GetTokens()
		if err != nil {
			t.Fatalf("GetTokens() error: %v", err)
		}
		if graph != "file-graph" || web != "file-web" || cookie != "file-cookie" {
			t.Errorf("GetTokens() from file = graph %q, web %q, cookie %q; want file-graph, file-web, file-cookie", graph, web, cookie)
		}
	})

	t.Run("environment overrides tokens file", func(t *testing.T) {
		configDir := isolateConfigDir(t)
		writeTokensFile(t, configDir, "MS_GRAPH_TOKEN=file-graph\nTEAMS_WEB_TOKEN=file-web\nTEAMS_COOKIE=file-cookie\n")
		clearEnv(t)
		t.Setenv("MS_GRAPH_TOKEN", "env-graph")
		t.Setenv("TEAMS_COOKIE", "env-cookie")

		graph, web, _, _, cookie, _, _, _, err := GetTokens()
		if err != nil {
			t.Fatalf("GetTokens() error: %v", err)
		}
		if graph != "env-graph" || web != "file-web" || cookie != "env-cookie" {
			t.Errorf("GetTokens() = graph %q, web %q, cookie %q; want env-graph, file-web, env-cookie", graph, web, cookie)
		}
	})
}
