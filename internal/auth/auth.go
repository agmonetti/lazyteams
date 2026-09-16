package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agmonetti/lazyteams/internal/helpers"
)

const (
	metadataKey   = "LAZYTEAMS_CREDENTIAL_METADATA"
	schemaVersion = 1
)

// Credential contains one secret and only metadata that can be safely used to
// decide whether a replacement is compatible. Tokens remain opaque.
type Credential struct {
	Token       string    `json:"-"`
	AcquiredAt  time.Time `json:"acquired_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Account     string    `json:"account,omitempty"`
	Tenant      string    `json:"tenant,omitempty"`
	Origin      string    `json:"origin,omitempty"`
	Resource    string    `json:"resource,omitempty"`
	Application string    `json:"application,omitempty"`
	Permissions []string  `json:"permissions,omitempty"`
}

// CredentialSet is immutable after publication. A request must load one set
// and select all credentials it needs from that same generation.
type CredentialSet struct {
	Version    int        `json:"version"`
	Generation uint64     `json:"generation"`
	AcquiredAt time.Time  `json:"acquired_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at,omitempty"`
	Graph      Credential `json:"graph"`
	Web        Credential `json:"web"`
	Notif      Credential `json:"notif"`
	EDU        Credential `json:"edu"`
	Cookie     Credential `json:"cookie"`
	EDUCookie  Credential `json:"edu_cookie"`
	Spaces     Credential `json:"spaces"`
	Fabric     Credential `json:"fabric"`
}

// TokenValues is the legacy wire shape used by the browser interceptor. It is
// converted to a CredentialSet only inside this package.
type TokenValues struct {
	Graph, Web, Notif, EDU string
	Cookie, EDUCookie      string
	Spaces, Fabric         string
}

type metadataFile struct {
	Version    int                `json:"version"`
	Generation uint64             `json:"generation"`
	AcquiredAt time.Time          `json:"acquired_at,omitempty"`
	ExpiresAt  time.Time          `json:"expires_at,omitempty"`
	Graph      credentialMetadata `json:"graph"`
	Web        credentialMetadata `json:"web"`
	Notif      credentialMetadata `json:"notif"`
	EDU        credentialMetadata `json:"edu"`
	Cookie     credentialMetadata `json:"cookie"`
	EDUCookie  credentialMetadata `json:"edu_cookie"`
	Spaces     credentialMetadata `json:"spaces"`
	Fabric     credentialMetadata `json:"fabric"`
}

type credentialMetadata struct {
	AcquiredAt  time.Time `json:"acquired_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Account     string    `json:"account,omitempty"`
	Tenant      string    `json:"tenant,omitempty"`
	Origin      string    `json:"origin,omitempty"`
	Resource    string    `json:"resource,omitempty"`
	Application string    `json:"application,omitempty"`
	Permissions []string  `json:"permissions,omitempty"`
}

func QuoteValue(v string) string { return strconv.Quote(v) }

func UnquoteValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if unquoted, err := strconv.Unquote(raw); err == nil {
		return unquoted
	}
	return strings.Trim(raw, `"`)
}

func credentialFromMetadata(token string, metadata credentialMetadata) Credential {
	return Credential{Token: normalizeToken(token), AcquiredAt: metadata.AcquiredAt, ExpiresAt: metadata.ExpiresAt, Account: metadata.Account, Tenant: metadata.Tenant, Origin: metadata.Origin, Resource: metadata.Resource, Application: metadata.Application, Permissions: append([]string(nil), metadata.Permissions...)}
}

func metadataFromCredential(c Credential) credentialMetadata {
	return credentialMetadata{AcquiredAt: c.AcquiredAt, ExpiresAt: c.ExpiresAt, Account: c.Account, Tenant: c.Tenant, Origin: c.Origin, Resource: c.Resource, Application: c.Application, Permissions: append([]string(nil), c.Permissions...)}
}

func normalizeToken(token string) string {
	return strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
}

func credentialFromToken(token string, acquiredAt time.Time) Credential {
	credential := Credential{Token: normalizeToken(token), AcquiredAt: acquiredAt, Origin: "browser"}
	parts := strings.Split(credential.Token, ".")
	if len(parts) < 2 {
		return credential
	}
	payload := parts[1]
	if padding := len(payload) % 4; padding != 0 {
		payload += strings.Repeat("=", 4-padding)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payload, "="))
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(payload)
	}
	if err != nil {
		return credential
	}
	var claims struct {
		OID string `json:"oid"`
		TID string `json:"tid"`
		AUD string `json:"aud"`
		APP string `json:"appid"`
		SCP string `json:"scp"`
	}
	if json.Unmarshal(decoded, &claims) != nil {
		return credential
	}
	credential.Account, credential.Tenant = claims.OID, claims.TID
	credential.Resource, credential.Application = claims.AUD, claims.APP
	credential.Permissions = strings.Fields(claims.SCP)
	return credential
}

func parseTokens(data []byte) (map[string]string, metadataFile) {
	values := make(map[string]string)
	var metadata metadataFile
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := UnquoteValue(parts[1])
		if key == metadataKey {
			_ = json.Unmarshal([]byte(value), &metadata)
			continue
		}
		values[key] = value
	}
	return values, metadata
}

func loadFromDir(configDir string) (*CredentialSet, error) {
	path := filepath.Join(configDir, "tokens.env")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &CredentialSet{Version: schemaVersion}, nil
		}
		return nil, err
	}
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "warning: %s is readable by other users (mode %#o); run chmod 600 on it\n", path, info.Mode().Perm())
	}
	values, metadata := parseTokens(data)
	set := &CredentialSet{
		Version: schemaVersion, Generation: metadata.Generation, AcquiredAt: metadata.AcquiredAt, ExpiresAt: metadata.ExpiresAt,
		Graph:     credentialFromMetadata(values["MS_GRAPH_TOKEN"], metadata.Graph),
		Web:       credentialFromMetadata(values["TEAMS_WEB_TOKEN"], metadata.Web),
		Notif:     credentialFromMetadata(values["TEAMS_NOTIF_TOKEN"], metadata.Notif),
		EDU:       credentialFromMetadata(values["EDU_TOKEN"], metadata.EDU),
		Cookie:    credentialFromMetadata(values["TEAMS_COOKIE"], metadata.Cookie),
		EDUCookie: credentialFromMetadata(values["EDU_COOKIE"], metadata.EDUCookie),
		Spaces:    credentialFromMetadata(values["TEAMS_SPACES_TOKEN"], metadata.Spaces),
		Fabric:    credentialFromMetadata(values["TEAMS_FABRIC_TOKEN"], metadata.Fabric),
	}
	if metadata.Version != 0 {
		set.Version = metadata.Version
	}
	return set, nil
}

// LoadCredentialSet loads one immutable snapshot. Environment overrides are
// memory-only and are marked as environment-origin credentials.
func LoadCredentialSet() (*CredentialSet, error) {
	set, err := loadFromDir(helpers.ConfigDir())
	if err != nil {
		return nil, err
	}
	return applyEnvironment(set), nil
}

func applyEnvironment(set *CredentialSet) *CredentialSet {
	set = clone(set)
	for key, target := range map[string]*Credential{
		"MS_GRAPH_TOKEN": &set.Graph, "TEAMS_WEB_TOKEN": &set.Web, "TEAMS_NOTIF_TOKEN": &set.Notif,
		"EDU_TOKEN": &set.EDU, "TEAMS_COOKIE": &set.Cookie, "EDU_COOKIE": &set.EDUCookie,
		"TEAMS_SPACES_TOKEN": &set.Spaces, "TEAMS_FABRIC_TOKEN": &set.Fabric,
	} {
		if value := os.Getenv(key); value != "" {
			*target = Credential{Token: normalizeToken(value), Origin: "environment"}
		}
	}
	return set
}

func clone(in *CredentialSet) *CredentialSet {
	out := *in
	for _, c := range []*Credential{&out.Graph, &out.Web, &out.Notif, &out.EDU, &out.Cookie, &out.EDUCookie, &out.Spaces, &out.Fabric} {
		c.Permissions = append([]string(nil), c.Permissions...)
	}
	return &out
}

func metadataFor(set *CredentialSet) metadataFile {
	return metadataFile{Version: schemaVersion, Generation: set.Generation, AcquiredAt: set.AcquiredAt, ExpiresAt: set.ExpiresAt,
		Graph: metadataFromCredential(set.Graph), Web: metadataFromCredential(set.Web), Notif: metadataFromCredential(set.Notif), EDU: metadataFromCredential(set.EDU), Cookie: metadataFromCredential(set.Cookie), EDUCookie: metadataFromCredential(set.EDUCookie), Spaces: metadataFromCredential(set.Spaces), Fabric: metadataFromCredential(set.Fabric)}
}

func serialize(set *CredentialSet) ([]byte, error) {
	metadata, err := json.Marshal(metadataFor(set))
	if err != nil {
		return nil, fmt.Errorf("marshal credential metadata: %w", err)
	}
	lines := []string{
		"# lazyteams tokens — auto-generated; do not edit manually",
		"export MS_GRAPH_TOKEN=" + QuoteValue(set.Graph.Token),
		"export TEAMS_WEB_TOKEN=" + QuoteValue(set.Web.Token),
		"export TEAMS_NOTIF_TOKEN=" + QuoteValue(set.Notif.Token),
		"export EDU_TOKEN=" + QuoteValue(set.EDU.Token),
		"export TEAMS_COOKIE=" + QuoteValue(set.Cookie.Token),
		"export EDU_COOKIE=" + QuoteValue(set.EDUCookie.Token),
		"export TEAMS_SPACES_TOKEN=" + QuoteValue(set.Spaces.Token),
		"export TEAMS_FABRIC_TOKEN=" + QuoteValue(set.Fabric.Token),
		"export " + metadataKey + "=" + QuoteValue(string(metadata)),
		"",
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tokens.env-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(content); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	if dirFile, openErr := os.Open(dir); openErr == nil {
		if err = dirFile.Sync(); err != nil {
			_ = dirFile.Close()
			return fmt.Errorf("sync credential directory: %w", err)
		}
		if err = dirFile.Close(); err != nil {
			return fmt.Errorf("close credential directory: %w", err)
		}
	}
	ok = true
	return nil
}

// GraphCandidate describes a token observed on a Graph Explorer request.
type GraphCandidate struct {
	Token      string
	RequestURL string
	PageURL    string
	ClientID   string
}

// ValidateGraphCandidate validates the untrusted acquisition boundary before persistence.
func ValidateGraphCandidate(candidate GraphCandidate, baseline *Credential) (Credential, error) {
	u, err := url.Parse(candidate.RequestURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "graph.microsoft.com" {
		return Credential{}, errors.New("graph request is not the official HTTPS endpoint")
	}
	page, err := url.Parse(candidate.PageURL)
	if err != nil || page.Scheme != "https" || page.Hostname() != "developer.microsoft.com" || !strings.Contains(page.Path, "/graph/graph-explorer") {
		return Credential{}, errors.New("graph token did not originate in Graph Explorer")
	}
	c := credentialFromToken(candidate.Token, time.Now().UTC())
	c.Origin = "graph_explorer"
	if c.Resource != "https://graph.microsoft.com" && c.Resource != "00000003-0000-0000-c000-000000000000" {
		return Credential{}, errors.New("graph token has incompatible audience")
	}
	if candidate.ClientID != "" && c.Application != candidate.ClientID {
		return Credential{}, errors.New("graph token has incompatible application")
	}
	required := map[string]bool{"User.Read": false, "Team.ReadBasic.All": false, "Channel.ReadBasic.All": false}
	chat := false
	for _, permission := range c.Permissions {
		if _, ok := required[permission]; ok {
			required[permission] = true
		}
		if permission == "Chat.ReadBasic" || permission == "Chat.Read" || permission == "Chat.ReadWrite" {
			chat = true
		}
	}
	for _, ok := range required {
		if !ok {
			return Credential{}, errors.New("graph token lacks bootstrap permissions")
		}
	}
	if !chat {
		return Credential{}, errors.New("graph token lacks chat permission")
	}
	if baseline != nil && !MetadataCompatible(*baseline, c) {
		return Credential{}, errors.New("graph token is incompatible with the current credential")
	}
	return c, nil
}

// PersistGraphCandidate validates and atomically persists a Graph Explorer token.
func PersistGraphCandidate(configDir string, candidate GraphCandidate) (*CredentialSet, error) {
	set, err := loadFromDir(configDir)
	if err != nil {
		return nil, err
	}
	var baseline *Credential
	if set.Graph.Token != "" {
		baseline = &set.Graph
	}
	c, err := ValidateGraphCandidate(candidate, baseline)
	if err != nil {
		return nil, err
	}
	return PersistDelta(configDir, CredentialDelta{BaseGeneration: set.Generation, Graph: &c})
}

type CredentialDelta struct {
	BaseGeneration uint64
	Graph          *Credential
	Web            *Credential
	Notif          *Credential
	EDU            *Credential
	Cookie         *Credential
	EDUCookie      *Credential
	Spaces         *Credential
	Fabric         *Credential
}

func mergeCredential(old Credential, incoming Credential) (Credential, error) {
	if !MetadataCompatible(old, incoming) {
		return Credential{}, errors.New("incompatible credential metadata")
	}
	out := old
	if incoming.Token != "" {
		out.Token = normalizeToken(incoming.Token)
	}
	if incoming.AcquiredAt.IsZero() == false {
		out.AcquiredAt = incoming.AcquiredAt
	}
	if incoming.ExpiresAt.IsZero() == false {
		out.ExpiresAt = incoming.ExpiresAt
	}
	if incoming.Account != "" {
		out.Account = incoming.Account
	}
	if incoming.Tenant != "" {
		out.Tenant = incoming.Tenant
	}
	if incoming.Origin != "" {
		out.Origin = incoming.Origin
	}
	if incoming.Resource != "" {
		out.Resource = incoming.Resource
	}
	if incoming.Application != "" {
		out.Application = incoming.Application
	}
	if len(incoming.Permissions) > 0 {
		out.Permissions = append([]string(nil), incoming.Permissions...)
	}
	return out, nil
}

// PersistDelta applies a generation-checked partial acquisition atomically.
func PersistDelta(configDir string, delta CredentialDelta) (*CredentialSet, error) {
	set, err := loadFromDir(configDir)
	if err != nil {
		return nil, err
	}
	if set.Generation != delta.BaseGeneration {
		return nil, fmt.Errorf("stale credential generation: have %d, want %d", set.Generation, delta.BaseGeneration)
	}
	next := clone(set)
	targets := []struct {
		incoming *Credential
		target   *Credential
	}{
		{delta.Graph, &next.Graph}, {delta.Web, &next.Web}, {delta.Notif, &next.Notif}, {delta.EDU, &next.EDU},
		{delta.Cookie, &next.Cookie}, {delta.EDUCookie, &next.EDUCookie}, {delta.Spaces, &next.Spaces}, {delta.Fabric, &next.Fabric},
	}
	for _, item := range targets {
		if item.incoming == nil {
			continue
		}
		merged, mergeErr := mergeCredential(*item.target, *item.incoming)
		if mergeErr != nil {
			return nil, mergeErr
		}
		*item.target = merged
	}
	next.Generation++
	next.AcquiredAt = time.Now().UTC()
	content, err := serialize(next)
	if err != nil {
		return nil, err
	}
	if err := atomicWrite(filepath.Join(configDir, "tokens.env"), content); err != nil {
		return nil, fmt.Errorf("persist credentials: %w", err)
	}
	return next, nil
}

// PersistTokenValues merges a partial browser capture with the current file
// and replaces the complete snapshot atomically. Empty values never erase a
// useful credential, including EDU_COOKIE.
func PersistTokenValues(configDir string, values TokenValues) (*CredentialSet, error) {
	set, err := loadFromDir(configDir)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	browserCredential := func(value string) *Credential {
		if value == "" {
			return nil
		}
		credential := credentialFromToken(value, now)
		return &credential
	}
	return PersistDelta(configDir, CredentialDelta{
		BaseGeneration: set.Generation,
		Graph:          browserCredential(values.Graph), Web: browserCredential(values.Web), Notif: browserCredential(values.Notif),
		EDU: browserCredential(values.EDU), Cookie: browserCredential(values.Cookie), EDUCookie: browserCredential(values.EDUCookie),
		Spaces: browserCredential(values.Spaces), Fabric: browserCredential(values.Fabric),
	})
}

// CloneCredentialSet returns a detached snapshot safe to publish or expose.
func CloneCredentialSet(in *CredentialSet) *CredentialSet {
	if in == nil {
		return &CredentialSet{Version: schemaVersion}
	}
	return clone(in)
}

// PersistCredentialSet is used by tests and future acquisition code after
// compatibility validation has already happened.
func PersistCredentialSet(configDir string, set *CredentialSet) error {
	if set == nil {
		return errors.New("nil credential set")
	}
	content, err := serialize(set)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(configDir, "tokens.env"), content)
}

func ParseUserNameFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(b, &claims); err != nil {
		return ""
	}
	for _, key := range []string{"name", "unique_name", "preferred_username", "upn", "given_name"} {
		if name, ok := claims[key].(string); ok && name != "" {
			return name
		}
	}
	return ""
}

// MetadataCompatible rejects a replacement when known identity or access
// boundaries contradict the existing credential. Unknown incoming metadata
// never erases known values.
func MetadataCompatible(old, incoming Credential) bool {
	pairs := [][2]string{{old.Account, incoming.Account}, {old.Tenant, incoming.Tenant}, {old.Origin, incoming.Origin}, {old.Resource, incoming.Resource}, {old.Application, incoming.Application}}
	for _, pair := range pairs {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			return false
		}
	}
	if len(old.Permissions) > 0 && len(incoming.Permissions) > 0 {
		incomingSet := map[string]bool{}
		for _, permission := range incoming.Permissions {
			incomingSet[permission] = true
		}
		for _, permission := range old.Permissions {
			if !incomingSet[permission] {
				return false
			}
		}
	}
	return true
}

// SortedPermissions provides deterministic metadata comparisons without
// exposing a mutable slice from a published snapshot.
func SortedPermissions(c Credential) []string {
	out := append([]string(nil), c.Permissions...)
	sort.Strings(out)
	return out
}


