package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
)

func TestSaveTokensPartialMergeKeepsEscapedValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.env")

	graph := "existing-graph-token"
	cookie := "line1\nline2\"quoted\"\\slash"
	initial := "# lazyteams tokens\n" +
		"export MS_GRAPH_TOKEN=" + auth.QuoteValue(graph) + "\n" +
		"export TEAMS_WEB_TOKEN=" + auth.QuoteValue("old-web") + "\n" +
		"export TEAMS_NOTIF_TOKEN=" + auth.QuoteValue("old-notif") + "\n" +
		"export TEAMS_COOKIE=" + auth.QuoteValue(cookie) + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	partial := &tokens{
		webToken:   "new-web",
		notifToken: "new-notif",
	}
	if err := saveTokens(partial, dir); err != nil {
		t.Fatalf("saveTokens: %v", err)
	}

	got := readTokensFileForTest(t, path)
	if got["MS_GRAPH_TOKEN"] != graph {
		t.Errorf("MS_GRAPH_TOKEN after partial renew = %q, want %q", got["MS_GRAPH_TOKEN"], graph)
	}
	if got["TEAMS_COOKIE"] != cookie {
		t.Errorf("TEAMS_COOKIE after partial renew = %q, want %q", got["TEAMS_COOKIE"], cookie)
	}
	if got["TEAMS_WEB_TOKEN"] != "new-web" || got["TEAMS_NOTIF_TOKEN"] != "new-notif" {
		t.Errorf("renewed tokens not applied: web=%q notif=%q", got["TEAMS_WEB_TOKEN"], got["TEAMS_NOTIF_TOKEN"])
	}
}

// readTokensFileForTest parses tokens.env the same way internal/auth's loader does.
func readTokensFileForTest(t *testing.T, path string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[strings.TrimSpace(parts[0])] = auth.UnquoteValue(parts[1])
		}
	}
	return result
}
