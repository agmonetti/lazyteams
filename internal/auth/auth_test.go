package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func jwtToken(claims map[string]interface{}) string {
	b, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func TestParseUserNameFromToken(t *testing.T) {
	tests := []struct{ name, token, want string }{
		{"name claim", jwtToken(map[string]interface{}{"name": "Alice Johnson"}), "Alice Johnson"},
		{"unique_name claim", jwtToken(map[string]interface{}{"unique_name": "alice@contoso.com"}), "alice@contoso.com"},
		{"preferred_username claim", jwtToken(map[string]interface{}{"preferred_username": "alice@contoso.com"}), "alice@contoso.com"},
		{"upn claim", jwtToken(map[string]interface{}{"upn": "alice@contoso.com"}), "alice@contoso.com"},
		{"given_name claim", jwtToken(map[string]interface{}{"given_name": "Alice"}), "Alice"},
		{"name wins", jwtToken(map[string]interface{}{"name": "Alice Johnson", "given_name": "Alice"}), "Alice Johnson"},
		{"no known claim", jwtToken(map[string]interface{}{"foo": "bar"}), ""},
		{"invalid token", "notajwt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseUserNameFromToken(tt.token); got != tt.want {
				t.Errorf("ParseUserNameFromToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func isolateConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return filepath.Join(dir, ".config", "lazyteams")
}

func TestLoadCredentialSetPreservesLegacyAndMetadata(t *testing.T) {
	dir := isolateConfigDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	set := &CredentialSet{Version: schemaVersion, Generation: 7, Graph: Credential{Token: "graph", Account: "a", Tenant: "t"}, EDUCookie: Credential{Token: "edu-cookie"}}
	if err := PersistCredentialSet(dir, set); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCredentialSet()
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 7 || got.Graph.Token != "graph" || got.Graph.Account != "a" || got.EDUCookie.Token != "edu-cookie" {
		t.Fatalf("loaded snapshot = %#v", got)
	}
}

func TestPersistTokenValuesMergesAtomicallyAndKeepsEDUCookie(t *testing.T) {
	dir := t.TempDir()
	initial := &CredentialSet{Version: schemaVersion, Generation: 3, Graph: Credential{Token: "old-graph"}, EDUCookie: Credential{Token: "keep-me"}}
	if err := PersistCredentialSet(dir, initial); err != nil {
		t.Fatal(err)
	}
	got, err := PersistTokenValues(dir, TokenValues{Web: "new-web"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 4 || got.Graph.Token != "old-graph" || got.Web.Token != "new-web" || got.EDUCookie.Token != "keep-me" {
		t.Fatalf("merged snapshot = %#v", got)
	}
	loaded, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != 4 || loaded.EDUCookie.Token != "keep-me" {
		t.Fatalf("persisted snapshot = %#v", loaded)
	}
	data, err := os.ReadFile(filepath.Join(dir, "tokens.env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "old-graph") == false {
		t.Fatal("atomic replacement did not preserve old graph value")
	}
}

func TestPersistCredentialSetFailureBeforeRenamePreservesSnapshot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not portable on Windows")
	}
	dir := t.TempDir()
	if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 1, Graph: Credential{Token: "stable"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tokens.env")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 2, Graph: Credential{Token: "replacement"}}); err == nil {
		t.Fatal("write unexpectedly succeeded in read-only directory")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed write changed the previous snapshot")
	}
}

func TestEnvironmentOverrideIsMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 1, Graph: Credential{Token: "file-graph"}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MS_GRAPH_TOKEN", "Bearer env-graph")
	set, err := loadFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	set = applyEnvironment(set)
	if set.Graph.Token != "env-graph" || set.Graph.Origin != "environment" {
		t.Fatalf("override = %#v", set.Graph)
	}
	persisted, err := loadFromDirWithoutEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Graph.Token != "file-graph" {
		t.Fatalf("environment override was persisted: %#v", persisted.Graph)
	}
}

func TestPersistDeltaRejectsStaleAndIncompatibleWithoutReplacement(t *testing.T) {
	dir := t.TempDir()
	initial := &CredentialSet{Version: schemaVersion, Generation: 8, Graph: Credential{Token: "stable", Account: "account-a"}}
	if err := PersistCredentialSet(dir, initial); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tokens.env")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PersistDelta(dir, CredentialDelta{BaseGeneration: 7, Graph: &Credential{Token: "new"}}); err == nil {
		t.Fatal("stale delta was accepted")
	}
	if _, err := PersistDelta(dir, CredentialDelta{BaseGeneration: 8, Graph: &Credential{Token: "new", Account: "account-b"}}); err == nil {
		t.Fatal("incompatible delta was accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected delta changed tokens.env")
	}
}

func loadFromDirWithoutEnv(dir string) (*CredentialSet, error) {
	keys := []string{"MS_GRAPH_TOKEN", "TEAMS_WEB_TOKEN", "TEAMS_NOTIF_TOKEN", "EDU_TOKEN", "TEAMS_COOKIE", "EDU_COOKIE", "TEAMS_SPACES_TOKEN", "TEAMS_FABRIC_TOKEN"}
	old := make(map[string]string, len(keys))
	for _, key := range keys {
		old[key] = os.Getenv(key)
		_ = os.Unsetenv(key)
	}
	defer func() {
		for _, key := range keys {
			_ = os.Setenv(key, old[key])
		}
	}()
	return loadFromDir(dir)
}

func TestValidateGraphCandidateRequiresOriginAndBootstrapPermissions(t *testing.T) {
	token := jwtToken(map[string]interface{}{
		"aud": "https://graph.microsoft.com", "oid": "acct", "tid": "tenant", "appid": GraphExplorerClientID,
		"scp": "User.Read Team.ReadBasic.All Channel.ReadBasic.All Chat.ReadBasic",
	})
	candidate := GraphCandidate{Token: token, RequestURL: "https://graph.microsoft.com/v1.0/me", PageURL: "https://developer.microsoft.com/en-us/graph/graph-explorer", ClientID: GraphExplorerClientID}
	got, err := ValidateGraphCandidate(candidate, nil)
	if err != nil || got.Origin != "graph_explorer" {
		t.Fatalf("valid candidate rejected: %#v, %v", got, err)
	}
	candidate.RequestURL = "https://graph.microsoft.com:443/v1.0/me"
	if _, err := ValidateGraphCandidate(candidate, nil); err == nil {
		t.Fatal("explicit Graph host port accepted")
	}
	candidate.RequestURL = "https://evil.example/graph.microsoft.com/v1.0/me"
	if _, err := ValidateGraphCandidate(candidate, nil); err == nil {
		t.Fatal("untrusted host accepted")
	}
	candidate.RequestURL = "https://graph.microsoft.com/v1.0/me"
	candidate.PageURL = "https://developer.microsoft.com:443/en-us/graph/graph-explorer"
	if _, err := ValidateGraphCandidate(candidate, nil); err == nil {
		t.Fatal("explicit Graph Explorer host port accepted")
	}
	candidate.PageURL = "https://teams.microsoft.com/"
	if _, err := ValidateGraphCandidate(candidate, nil); err == nil {
		t.Fatal("untrusted origin accepted")
	}
}

func TestPersistGraphCandidateReturnsPersistedGeneration(t *testing.T) {
	dir := t.TempDir()
	if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 4}); err != nil {
		t.Fatal(err)
	}
	candidate := GraphCandidate{
		Token:      jwtToken(map[string]interface{}{"aud": "https://graph.microsoft.com", "oid": "acct", "tid": "tenant", "appid": GraphExplorerClientID, "scp": "User.Read Team.ReadBasic.All Channel.ReadBasic.All Chat.ReadBasic"}),
		RequestURL: "https://graph.microsoft.com/v1.0/me", PageURL: "https://developer.microsoft.com/en-us/graph/graph-explorer", ClientID: GraphExplorerClientID,
	}
	got, err := PersistGraphCandidate(dir, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 5 || got.Graph.Token == "" {
		t.Fatalf("persisted snapshot = generation %d, graph token present=%t", got.Generation, got.Graph.Token != "")
	}
}

func TestPersistTokenValuesDoesNotPersistFabricFromGenericCapture(t *testing.T) {
	dir := t.TempDir()
	before := &CredentialSet{Version: schemaVersion, Generation: 4, Fabric: Credential{Token: "stable-fabric"}}
	if err := PersistCredentialSet(dir, before); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tokens.env")
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PersistTokenValues(dir, TokenValues{Fabric: "unverified-fabric"})
	if !errors.Is(err, ErrIncompatibleCredential) {
		t.Fatalf("expected generic Fabric capture rejection, got %v", err)
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Fatal("generic Fabric capture changed snapshot")
	}
}

func TestValidateFabricCandidateFailsClosedWithoutWritePolicy(t *testing.T) {
	candidate := Credential{
		Token: "candidate", Origin: "fabric_write_intercept", Resource: "observed-resource",
		Application: "observed-application", Account: "acct", Tenant: "tenant", Permissions: []string{"read-scope"},
	}
	if _, err := ValidateFabricCandidate(candidate, nil); !errors.Is(err, ErrIncompatibleCredential) {
		t.Fatalf("Fabric candidate accepted without contractual write policy: %v", err)
	}
}

func TestPersistFabricCandidateRejectsEveryMetadataVariantWithoutChangingFile(t *testing.T) {
	baseline := Credential{
		Token: "stable", Origin: "previous-origin", Resource: "previous-resource",
		Application: "previous-application", Account: "acct-a", Tenant: "tenant-a", Permissions: []string{"previous-permission"},
	}
	candidates := map[string]Credential{
		"apparent complete metadata": {Token: "replacement", Origin: "fabric_write_intercept", Resource: "observed-resource", Application: "observed-application", Account: "acct-a", Tenant: "tenant-a", Permissions: []string{"apparent-write-scope"}},
		"resource mismatch":          {Token: "replacement", Origin: baseline.Origin, Resource: "other-resource", Application: baseline.Application, Account: baseline.Account, Tenant: baseline.Tenant, Permissions: baseline.Permissions},
		"application mismatch":       {Token: "replacement", Origin: baseline.Origin, Resource: baseline.Resource, Application: "other-application", Account: baseline.Account, Tenant: baseline.Tenant, Permissions: baseline.Permissions},
		"account mismatch":           {Token: "replacement", Origin: baseline.Origin, Resource: baseline.Resource, Application: baseline.Application, Account: "acct-b", Tenant: baseline.Tenant, Permissions: baseline.Permissions},
		"tenant mismatch":            {Token: "replacement", Origin: baseline.Origin, Resource: baseline.Resource, Application: baseline.Application, Account: baseline.Account, Tenant: "tenant-b", Permissions: baseline.Permissions},
		"provenance mismatch":        {Token: "replacement", Origin: "browser", Resource: baseline.Resource, Application: baseline.Application, Account: baseline.Account, Tenant: baseline.Tenant, Permissions: baseline.Permissions},
		"permission mismatch":        {Token: "replacement", Origin: baseline.Origin, Resource: baseline.Resource, Application: baseline.Application, Account: baseline.Account, Tenant: baseline.Tenant, Permissions: []string{"read-only"}},
	}
	for name, candidate := range candidates {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 2, Fabric: baseline}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "tokens.env")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := PersistFabricCandidate(dir, candidate); !errors.Is(err, ErrIncompatibleCredential) {
				t.Fatalf("Fabric candidate accepted without write policy: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("rejected Fabric candidate changed snapshot")
			}
		})
	}
}

func TestPersistTokenValuesDoesNotAdvanceGenerationForEmptyDelta(t *testing.T) {
	dir := t.TempDir()
	before := &CredentialSet{Version: schemaVersion, Generation: 4, Graph: Credential{Token: "stable"}}
	if err := PersistCredentialSet(dir, before); err != nil {
		t.Fatal(err)
	}
	got, err := PersistTokenValues(dir, TokenValues{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != before.Generation || got.Graph.Token != before.Graph.Token {
		t.Fatalf("empty delta changed snapshot: generation=%d token=%q", got.Generation, got.Graph.Token)
	}
}

func TestValidateGraphCandidateRequiresGraphExplorerClientID(t *testing.T) {
	candidate := GraphCandidate{
		Token:      jwtToken(map[string]interface{}{"aud": "https://graph.microsoft.com", "oid": "acct", "tid": "tenant", "appid": GraphExplorerClientID, "scp": "User.Read Team.ReadBasic.All Channel.ReadBasic.All Chat.ReadBasic"}),
		RequestURL: "https://graph.microsoft.com/v1.0/me", PageURL: "https://developer.microsoft.com/en-us/graph/graph-explorer", ClientID: "wrong-client",
	}
	if _, err := ValidateGraphCandidate(candidate, nil); err == nil {
		t.Fatal("candidate from an unknown client was accepted")
	}
}

func TestPersistGraphCandidateSkipsInvalidThenAcceptsValid(t *testing.T) {
	dir := t.TempDir()
	if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 4, Graph: Credential{Token: "stable"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tokens.env")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := GraphCandidate{
		Token:      jwtToken(map[string]interface{}{"aud": "https://graph.microsoft.com", "oid": "acct", "tid": "tenant", "appid": GraphExplorerClientID, "scp": "User.Read"}),
		RequestURL: "https://graph.microsoft.com/v1.0/me", PageURL: "https://developer.microsoft.com/en-us/graph/graph-explorer", ClientID: GraphExplorerClientID,
	}
	if _, err := PersistGraphCandidate(dir, invalid); err == nil {
		t.Fatal("invalid candidate was persisted")
	}
	afterInvalid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterInvalid) != string(before) {
		t.Fatal("invalid candidate changed the snapshot")
	}
	valid := invalid
	valid.Token = jwtToken(map[string]interface{}{"aud": "https://graph.microsoft.com", "oid": "acct", "tid": "tenant", "appid": GraphExplorerClientID, "scp": "User.Read Team.ReadBasic.All Channel.ReadBasic.All Chat.ReadBasic"})
	got, err := PersistGraphCandidate(dir, valid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 5 || got.Graph.Token == "stable" {
		t.Fatalf("valid candidate was not persisted: generation=%d token=%q", got.Generation, got.Graph.Token)
	}
}

func TestPersistGraphCandidateRejectsDowngradeWithoutChangingFile(t *testing.T) {
	dir := t.TempDir()
	old := Credential{Token: "old", Account: "acct", Tenant: "tenant", Resource: "https://graph.microsoft.com", Permissions: []string{"User.Read", "Chat.Read"}}
	if err := PersistCredentialSet(dir, &CredentialSet{Version: schemaVersion, Generation: 1, Graph: old}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "tokens.env"))
	candidate := GraphCandidate{Token: jwtToken(map[string]interface{}{"aud": "https://graph.microsoft.com", "oid": "acct", "tid": "tenant", "appid": GraphExplorerClientID, "scp": "User.Read Team.ReadBasic.All Channel.ReadBasic.All Chat.ReadBasic"}), RequestURL: "https://graph.microsoft.com/v1.0/me", PageURL: "https://developer.microsoft.com/en-us/graph/graph-explorer", ClientID: GraphExplorerClientID}
	if _, err := PersistGraphCandidate(dir, candidate); err == nil {
		t.Fatal("candidate with unknown baseline permissions should not erase known permissions")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "tokens.env"))
	if string(before) != string(after) {
		t.Fatal("rejected candidate changed snapshot")
	}
}

func TestMetadataCompatibleAllowsGraphExplorerOriginUpgrade(t *testing.T) {
	old := Credential{Origin: "browser"}
	if !MetadataCompatible(old, Credential{Origin: "graph_explorer"}) {
		t.Fatal("graph explorer should strengthen legacy browser origin")
	}
	if MetadataCompatible(Credential{Origin: "graph_explorer"}, Credential{Origin: "browser"}) {
		t.Fatal("generic browser must not degrade graph explorer origin")
	}
}

func TestValidateGraphCandidateRejectsUnknownBaselineIdentity(t *testing.T) {
	token := jwtToken(map[string]interface{}{
		"aud": "https://graph.microsoft.com", "scp": "User.Read Team.ReadBasic.All Channel.ReadBasic.All Chat.ReadBasic",
	})
	candidate := GraphCandidate{Token: token, RequestURL: "https://graph.microsoft.com/v1.0/me", PageURL: "https://developer.microsoft.com/en-us/graph/graph-explorer"}
	if _, err := ValidateGraphCandidate(candidate, &Credential{Account: "known"}); err == nil {
		t.Fatal("candidate with unknown account metadata replaced known identity")
	}
}

func TestMetadataCompatibleRejectsIdentityMismatchAndPermissionDowngrade(t *testing.T) {
	if MetadataCompatible(Credential{Account: "a"}, Credential{Account: "b"}) {
		t.Fatal("account mismatch accepted")
	}
	if !MetadataCompatible(Credential{Account: "a"}, Credential{}) {
		t.Fatal("unknown incoming account rejected")
	}
	old := Credential{Permissions: []string{"User.Read", "Chat.Read"}}
	if MetadataCompatible(old, Credential{Permissions: []string{"User.Read"}}) {
		t.Fatal("permission downgrade accepted")
	}
	if !MetadataCompatible(old, Credential{Permissions: []string{"User.Read", "Chat.Read", "Files.Read"}}) {
		t.Fatal("permission superset rejected")
	}
}

func TestCloneCredentialSetDetachesPermissions(t *testing.T) {
	set := &CredentialSet{Graph: Credential{Token: "token", Permissions: []string{"User.Read"}}}
	clone := CloneCredentialSet(set)
	clone.Graph.Token = "changed"
	clone.Graph.Permissions[0] = "changed"
	if set.Graph.Token != "token" || set.Graph.Permissions[0] != "User.Read" {
		t.Fatal("clone aliases published credential data")
	}
}
