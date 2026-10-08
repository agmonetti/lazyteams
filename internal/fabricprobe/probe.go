// Package fabricprobe contains the fail-closed, network-free core of the
// isolated Stage 7B Fabric probe. It never persists or exposes credentials.
package fabricprobe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
)

const teamsOrigin = "https://teams.microsoft.com"

type State string

const (
	Prepared                 State = "prepared"
	RouteInstalled           State = "route_installed"
	AuditBeforeOK            State = "audit_before_ok"
	TargetAborted            State = "target_aborted"
	RequestFailedConfirmed   State = "request_failed_confirmed"
	ContextClosed            State = "context_closed"
	AuditAfterOK             State = "audit_after_ok"
	WriteCandidateUnverified State = "write_candidate_unverified"
	Rejected                 State = "rejected"
)

type Decision uint8

const (
	Continue Decision = iota
	AbortTarget
	AbortUnknownMutation
)

type RejectReason string

const (
	RejectInvalidTarget    RejectReason = "invalid_target"
	RejectInvalidOrder     RejectReason = "invalid_order"
	RejectUnknownMutation  RejectReason = "unknown_mutation"
	RejectAbortFailed      RejectReason = "abort_failed"
	RejectAmbiguousEvent   RejectReason = "ambiguous_event"
	RejectCandidateChanged RejectReason = "candidate_changed"
	RejectResponseObserved RejectReason = "response_observed"
	RejectAuditChanged     RejectReason = "audit_changed"
	RejectAuditIncomplete  RejectReason = "audit_incomplete"
	RejectCancelled        RejectReason = "cancelled"
	RejectTimeout          RejectReason = "timeout"
)

type Target struct {
	Team    string
	Channel string
	User    string
	Role    int
}

type Request struct {
	ID             string
	Method         string
	URL            string
	FrameURL       string
	FrameRelation  string
	FrameAncestors []string
	ContentType    string
	Body           []byte
	Candidate      string
}

type Audit struct {
	Exists bool
	Role   string
}

type RequestFingerprint struct {
	Method           string   `json:"method"`
	Route            string   `json:"route"`
	Query            string   `json:"query"`
	ContentType      string   `json:"content_type"`
	BodySchema       string   `json:"body_schema"`
	Frame            string   `json:"frame"`
	FrameRelation    string   `json:"frame_relation"`
	FrameAncestors   []string `json:"frame_ancestors,omitempty"`
	SchemeHTTPS      bool     `json:"scheme_https"`
	HostClass        string   `json:"host_class"`
	HostMatch        bool     `json:"host_match"`
	PathMatch        bool     `json:"path_match"`
	PortEmpty        bool     `json:"port_empty"`
	UserinfoAbsent   bool     `json:"userinfo_absent"`
	FragmentAbsent   bool     `json:"fragment_absent"`
	QueryMatch       bool     `json:"query_match"`
	PayloadMatch     bool     `json:"payload_match"`
	CandidatePresent bool     `json:"candidate_present"`
}

type Evidence struct {
	State             State               `json:"state"`
	Method            string              `json:"method,omitempty"`
	Route             string              `json:"route,omitempty"`
	Attempts          int                 `json:"attempts"`
	Aborted           bool                `json:"aborted"`
	RequestFailed     bool                `json:"request_failed"`
	ResponseObserved  bool                `json:"response_observed"`
	ContextClosed     bool                `json:"context_closed"`
	AuditAfter        bool                `json:"audit_after"`
	SnapshotUnchanged bool                `json:"snapshot_unchanged"`
	Unchanged         bool                `json:"unchanged"`
	ErrorCode         RejectReason        `json:"error_code,omitempty"`
	Mutation          *RequestFingerprint `json:"mutation,omitempty"`
}

type Probe struct {
	mu               sync.Mutex
	target           Target
	state            State
	reason           RejectReason
	before           Audit
	mutation         *RequestFingerprint
	candidate        string
	eligible         map[string]string
	failedEarly      map[string]bool
	attempts         map[string]bool
	unknown          map[string]bool
	observed         map[string]bool
	uncorrelated     bool
	aborted          bool
	failed           bool
	closed           bool
	audited          bool
	snapshotOK       bool
	responseObserved bool
}

func New(target Target) (*Probe, error) {
	if !validSegment(target.Team) || !validSegment(target.Channel) || !validSegment(target.User) || (target.Role != 1 && target.Role != 2) {
		return nil, errors.New(string(RejectInvalidTarget))
	}
	return &Probe{target: target, state: Prepared, eligible: make(map[string]string), failedEarly: make(map[string]bool), attempts: make(map[string]bool), unknown: make(map[string]bool), observed: make(map[string]bool)}, nil
}

func validSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/?#")
}

func (p *Probe) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Probe) Reason() RejectReason {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reason
}

func (p *Probe) InstallRoute() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != Prepared {
		return p.reject(RejectInvalidOrder)
	}
	p.state = RouteInstalled
	return true
}

func (p *Probe) AuditBefore(audit Audit) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != RouteInstalled {
		return p.reject(RejectInvalidOrder)
	}
	if !validAudit(audit) {
		return p.reject(RejectAuditIncomplete)
	}
	p.before = audit
	p.state = AuditBeforeOK
	return true
}

// Classify returns the only safe routing action for a request. Every
// non-idempotent Fabric request is aborted; only the exact configured request
// is eligible to provide a quarantined candidate.
func (p *Probe) Classify(req Request) Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	u, err := url.Parse(req.URL)
	if err != nil || !strings.HasPrefix(path.Clean(u.Path), "/fabric/") {
		return Continue
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead || req.Method == http.MethodOptions {
		return Continue
	}
	if req.ID == "" || p.observed[req.ID] {
		p.uncorrelated = true
	} else {
		p.observed[req.ID] = true
	}
	if p.state == Rejected {
		if p.reason == RejectUnknownMutation {
			p.reason = RejectAmbiguousEvent
		}
		if p.reason == RejectAmbiguousEvent {
			p.mutation = p.sanitizeRequest(req, u)
			if req.ID != "" {
				p.unknown[req.ID] = true
			}
		}
		return AbortUnknownMutation
	}
	if p.state != AuditBeforeOK && p.state != TargetAborted && p.state != RequestFailedConfirmed {
		p.reject(RejectInvalidOrder)
		return AbortUnknownMutation
	}
	if !p.matchesTarget(req, u) || req.ID == "" {
		p.mutation = p.sanitizeRequest(req, u)
		if len(p.attempts) > 0 || len(p.eligible) > 0 || len(p.failedEarly) > 0 {
			for id := range p.eligible {
				p.unknown[id] = true // Keep IDs, never candidate values, until Abort resolves.
			}
			p.reject(RejectAmbiguousEvent)
			if req.ID != "" {
				p.unknown[req.ID] = true
			}
			return AbortUnknownMutation
		}
		if req.ID != "" {
			p.unknown[req.ID] = true
		}
		p.reason = RejectUnknownMutation
		p.state = Rejected
		return AbortUnknownMutation
	}
	if _, exists := p.eligible[req.ID]; exists {
		p.reject(RejectAmbiguousEvent)
		return AbortUnknownMutation
	}
	if _, exists := p.attempts[req.ID]; exists {
		p.reject(RejectAmbiguousEvent)
		return AbortUnknownMutation
	}
	p.eligible[req.ID] = req.Candidate
	return AbortTarget
}

func (p *Probe) matchesTarget(req Request, u *url.URL) bool {
	schemeHTTPS, _, hostMatch, pathMatch, portEmpty, userinfoAbsent, fragmentAbsent, queryMatch, payloadMatch, candidatePresent := p.targetMatches(req, u)
	endpoint := schemeHTTPS && hostMatch && pathMatch && portEmpty && userinfoAbsent && fragmentAbsent
	frame, err := url.Parse(req.FrameURL)
	frameMatch := err == nil && frame.Scheme == "https" && frame.Host == "teams.microsoft.com" && frame.Port() == "" && frame.User == nil
	relationKnown := req.FrameRelation == "main" || req.FrameRelation == "child"
	return req.Method == http.MethodPost && endpoint && queryMatch && payloadMatch && candidatePresent && frameMatch && relationKnown
}

func (p *Probe) targetMatches(req Request, u *url.URL) (schemeHTTPS bool, hostClass string, hostMatch, pathMatch, portEmpty, userinfoAbsent, fragmentAbsent, queryMatch, payloadMatch, candidatePresent bool) {
	schemeHTTPS = u.Scheme == "https"
	hostname := u.Hostname()
	switch hostname {
	case "teams.microsoft.com":
		hostClass = "teams.microsoft.com"
	case "teams.cloud.microsoft":
		hostClass = "teams.cloud.microsoft"
	case "":
		hostClass = "invalid"
	default:
		hostClass = "other"
	}
	hostMatch = hostname == "teams.microsoft.com"
	portEmpty = u.Port() == ""
	userinfoAbsent = u.User == nil
	fragmentAbsent = u.Fragment == ""

	expectedPath := "/fabric/amer/templates/api/teams/" + url.PathEscape(p.target.Team) + "/channels/" + url.PathEscape(p.target.Channel) + "/users"
	pathMatch = u.EscapedPath() == expectedPath

	queryMatch = u.RawQuery == "forceSync=false"
	candidatePresent = req.Candidate != ""
	var parsed struct {
		Users []struct {
			ID   string `json:"id"`
			Role int    `json:"role"`
		} `json:"users"`
	}
	duplicate, err := duplicateJSONKey(req.Body)
	if err != nil || duplicate {
		return schemeHTTPS, hostClass, hostMatch, pathMatch, portEmpty, userinfoAbsent, fragmentAbsent, queryMatch, false, candidatePresent
	}
	decoder := json.NewDecoder(bytes.NewReader(req.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&parsed) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return schemeHTTPS, hostClass, hostMatch, pathMatch, portEmpty, userinfoAbsent, fragmentAbsent, queryMatch, false, candidatePresent
	}
	payloadMatch = len(parsed.Users) == 1 && parsed.Users[0].ID == p.target.User && parsed.Users[0].Role == p.target.Role
	return schemeHTTPS, hostClass, hostMatch, pathMatch, portEmpty, userinfoAbsent, fragmentAbsent, queryMatch, payloadMatch, candidatePresent
}

// Aborted records a successful Route.Abort for a request previously matched
// by Classify. The candidate never leaves the probe.
func (p *Probe) Aborted(requestID string, abortErr error) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == Rejected && (p.reason == RejectUnknownMutation || p.reason == RejectAmbiguousEvent) && p.unknown[requestID] {
		if abortErr != nil {
			p.reject(RejectAbortFailed)
			return false
		}
		delete(p.unknown, requestID)
		p.aborted = true
		failed := p.failedEarly[requestID]
		delete(p.failedEarly, requestID)
		p.attempts[requestID] = failed
		if failed {
			p.failed = true
		}
		return true
	}
	if p.state == Rejected {
		return false
	}
	if abortErr != nil {
		return p.reject(RejectAbortFailed)
	}
	candidate, eligible := p.eligible[requestID]
	if (p.state != AuditBeforeOK && p.state != TargetAborted && p.state != RequestFailedConfirmed) || !eligible {
		return p.reject(RejectAmbiguousEvent)
	}
	delete(p.eligible, requestID)
	if p.candidate != "" && p.candidate != candidate {
		return p.reject(RejectCandidateChanged)
	}
	p.candidate = candidate
	p.aborted = true
	p.attempts[requestID] = p.failedEarly[requestID]
	delete(p.failedEarly, requestID)
	if p.attempts[requestID] {
		p.state = RequestFailedConfirmed
	} else {
		p.state = TargetAborted
	}
	return true
}

func (p *Probe) RequestFailed(requestID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == Rejected && (p.reason == RejectUnknownMutation || p.reason == RejectAmbiguousEvent) {
		if p.unknown[requestID] && !p.failedEarly[requestID] {
			p.failedEarly[requestID] = true
			return true
		}
		if failed, exists := p.attempts[requestID]; exists && !failed && p.aborted {
			p.attempts[requestID] = true
			p.failed = true
			return true
		}
		return p.reject(RejectAmbiguousEvent)
	}
	if p.state == Rejected {
		return false
	}
	failed, aborted := p.attempts[requestID]
	if aborted {
		if failed {
			return p.reject(RejectAmbiguousEvent)
		}
		p.attempts[requestID] = true
		p.failed = true
		p.state = RequestFailedConfirmed
		return true
	}
	if _, eligible := p.eligible[requestID]; eligible && !p.failedEarly[requestID] {
		p.failedEarly[requestID] = true
		p.failed = true
		return true
	}
	return p.reject(RejectAmbiguousEvent)
}

func (p *Probe) Response(requestID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.observed[requestID] {
		p.responseObserved = true
		p.reject(RejectResponseObserved)
		return false
	}
	return true
}

func (p *Probe) CloseContext() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == Rejected {
		p.closed = true
		if p.reason == RejectUnknownMutation && p.aborted && p.failed && len(p.unknown) == 0 && len(p.attempts) == 1 && len(p.observed) == 1 {
			p.state = ContextClosed
			return true
		}
		return false
	}
	if p.state != RequestFailedConfirmed || len(p.attempts) == 0 || len(p.eligible) != 0 {
		return p.reject(RejectInvalidOrder)
	}
	for _, failed := range p.attempts {
		if !failed {
			return p.reject(RejectAmbiguousEvent)
		}
	}
	p.closed = true
	p.state = ContextClosed
	return true
}

func (p *Probe) AuditAfter(audit Audit) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != ContextClosed {
		return p.reject(RejectInvalidOrder)
	}
	if !validAudit(audit) {
		return p.reject(RejectAuditIncomplete)
	}
	if audit != p.before {
		return p.reject(RejectAuditChanged)
	}
	p.audited = true
	p.state = AuditAfterOK
	return true
}

func (p *Probe) FinalizeDiagnostic(snapshotUnchanged bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != AuditAfterOK || p.reason != RejectUnknownMutation {
		return p.reject(RejectInvalidOrder)
	}
	p.closed = true
	p.audited = true
	p.snapshotOK = snapshotUnchanged
	if !snapshotUnchanged {
		return p.reject(RejectSnapshotChanged)
	}
	p.state = Rejected
	return false
}

func (p *Probe) DiagnosticReady() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reason == RejectUnknownMutation && p.aborted && p.failed && len(p.unknown) == 0 && len(p.attempts) == 1 && len(p.observed) == 1
}

func (p *Probe) Finalize(snapshotUnchanged bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != AuditAfterOK {
		return p.reject(RejectInvalidOrder)
	}
	p.snapshotOK = snapshotUnchanged
	if !snapshotUnchanged {
		return p.reject(RejectSnapshotChanged)
	}
	p.candidate = ""
	p.state = WriteCandidateUnverified
	return true
}

func (p *Probe) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reject(RejectCancelled)
}

func (p *Probe) Timeout() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reject(RejectTimeout)
}

func (p *Probe) Evidence() Evidence {
	p.mu.Lock()
	defer p.mu.Unlock()
	unchanged := p.audited && p.snapshotOK
	// These flags describe all observed mutations, not only the last fingerprint.
	aborted := !p.uncorrelated && len(p.observed) > 0 && len(p.observed) == len(p.attempts) && len(p.unknown) == 0 && len(p.eligible) == 0
	failed := aborted
	for _, confirmed := range p.attempts {
		failed = failed && confirmed
	}
	evidence := Evidence{State: p.state, Attempts: len(p.attempts), Aborted: aborted, RequestFailed: failed, ResponseObserved: p.responseObserved, ContextClosed: p.closed, AuditAfter: p.audited, SnapshotUnchanged: p.snapshotOK, Unchanged: unchanged, ErrorCode: p.reason, Mutation: p.mutation}
	if len(p.attempts) > 0 {
		if p.mutation != nil {
			evidence.Method = p.mutation.Method
			evidence.Route = p.mutation.Route
		} else {
			evidence.Method = http.MethodPost
			evidence.Route = "/fabric/amer/templates/api/teams/{team}/channels/{channel}/users"
		}
	}
	return evidence
}

func (p *Probe) sanitizeRequest(req Request, u *url.URL) *RequestFingerprint {
	schemeHTTPS, hostClass, hostMatch, pathMatch, portEmpty, userinfoAbsent, fragmentAbsent, queryMatch, payloadMatch, candidatePresent := p.targetMatches(req, u)
	fingerprint := &RequestFingerprint{
		Method:           sanitizedMethod(req.Method),
		Route:            sanitizedRoute(u.Path),
		Query:            sanitizedQuery(u.Query()),
		ContentType:      sanitizedContentType(req.ContentType),
		BodySchema:       bodySchema(req.Body),
		Frame:            frameClass(req.FrameURL),
		FrameRelation:    frameRelation(req.FrameRelation),
		FrameAncestors:   ancestorClasses(req.FrameAncestors),
		SchemeHTTPS:      schemeHTTPS,
		HostClass:        hostClass,
		HostMatch:        hostMatch,
		PathMatch:        pathMatch,
		PortEmpty:        portEmpty,
		UserinfoAbsent:   userinfoAbsent,
		FragmentAbsent:   fragmentAbsent,
		QueryMatch:       queryMatch,
		PayloadMatch:     payloadMatch,
		CandidatePresent: candidatePresent,
	}
	if fingerprintSize(fingerprint) > maxFingerprintBytes {
		fingerprint.BodySchema = "fingerprint_too_large"
	}
	return fingerprint
}

func frameClass(raw string) string {
	frame, err := url.Parse(raw)
	if err != nil || raw == "" {
		return "opaque_or_missing"
	}
	if frame.Scheme == "" || frame.Host == "" {
		return "opaque_or_missing"
	}
	if frame.Scheme != "https" || frame.Port() != "" || frame.User != nil {
		return "other_origin"
	}
	switch frame.Host {
	case "teams.microsoft.com":
		return "teams.microsoft.com"
	case "teams.cloud.microsoft":
		return "teams.cloud.microsoft"
	default:
		return "other_origin"
	}
}

func frameRelation(relation string) string {
	if relation == "main" || relation == "child" {
		return relation
	}
	return "indeterminate"
}

func ancestorClasses(ancestors []string) []string {
	if len(ancestors) == 0 {
		return nil
	}
	if len(ancestors) > 4 {
		ancestors = ancestors[:4]
	}
	classes := make([]string, len(ancestors))
	for i, ancestor := range ancestors {
		classes[i] = frameClass(ancestor)
	}
	return classes
}

func sanitizedMethod(method string) string {
	switch method = strings.ToUpper(method); method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return method
	default:
		return "other"
	}
}

func sanitizedRoute(rawPath string) string {
	if len(rawPath) > maxRouteInputBytes {
		return "route_too_large"
	}
	parts := strings.Split(strings.Trim(path.Clean(rawPath), "/"), "/")
	static := map[string]bool{"fabric": true, "amer": true, "templates": true, "api": true, "teams": true, "channels": true, "users": true}
	for i, part := range parts {
		if !static[part] {
			parts[i] = "{param}"
		}
	}
	if len(parts) == 0 || parts[0] == "" {
		return "/"
	}
	route := "/" + strings.Join(parts, "/")
	if len(route) > maxRouteBytes {
		return fmt.Sprintf("route[%d]{<more>}", len(parts))
	}
	return route
}

func fingerprintSize(fingerprint *RequestFingerprint) int {
	return len(fingerprint.Method) + len(fingerprint.Route) + len(fingerprint.Query) + len(fingerprint.ContentType) + len(fingerprint.BodySchema) + len(fingerprint.Frame)
}

func sanitizedQuery(values url.Values) string {
	allowed := map[string]bool{"forceSync": true, "view": true, "pageSize": true, "selectMemberRoles": true}
	keys := make([]string, 0, len(values))
	for key := range values {
		if allowed[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func sanitizedContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	switch value {
	case "application/json", "application/x-www-form-urlencoded", "multipart/form-data", "text/plain":
		return value
	default:
		if value == "" {
			return ""
		}
		return "other"
	}
}

const (
	maxBodyBytes         = 64 << 10
	maxSchemaBytes       = 2048
	maxSchemaDepth       = 8
	maxArrayItems        = 16
	maxObjectFields      = 32
	maxRouteInputBytes   = 2048
	maxRouteBytes        = 512
	maxFingerprintBytes  = 2304
	maxEvidenceJSONBytes = 3072
)

func bodySchema(body []byte) string {
	if len(body) > maxBodyBytes {
		return "body_too_large"
	}
	duplicate, err := duplicateJSONKey(body)
	if err != nil {
		return "invalid_json"
	}
	if duplicate {
		return "duplicate_keys"
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return "invalid_json"
	}
	schema, exceeded := schemaOf(value)
	if exceeded {
		return "schema_too_large"
	}
	return schema
}

// schemaKey is deliberately small: an unrecognised JSON key must never be
// copied to evidence because Teams can put identifiers in object keys.
func schemaKey(key string) string {
	switch key {
	case "users", "id", "role", "note":
		return key
	default:
		return "<key>"
	}
}

type schemaWriter struct {
	strings.Builder
	exceeded bool
}

func (w *schemaWriter) write(value string) {
	if w.exceeded || w.Len()+len(value) > maxSchemaBytes {
		w.exceeded = true
		return
	}
	w.WriteString(value)
}

func schemaOf(value any) (string, bool) {
	var writer schemaWriter
	writeSchema(&writer, value, 0)
	return writer.String(), writer.exceeded
}

func writeSchema(writer *schemaWriter, value any, depth int) {
	if writer.exceeded {
		return
	}
	if depth >= maxSchemaDepth {
		writer.write("<depth>")
		return
	}
	switch value := value.(type) {
	case nil:
		writer.write("null")
	case bool:
		writer.write("bool")
	case json.Number:
		writer.write("number")
	case string:
		writer.write("string")
	case []any:
		writer.write(fmt.Sprintf("array[%d]{", len(value)))
		for i, item := range value {
			if i == maxArrayItems {
				writer.write("<more>")
				break
			}
			if i > 0 {
				writer.write(",")
			}
			writeSchema(writer, item, depth+1)
		}
		writer.write("}")
	case map[string]any:
		if len(value) > maxObjectFields {
			writer.write(fmt.Sprintf("object[%d]{<more>}", len(value)))
			return
		}
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		writer.write("object{")
		for i, key := range keys {
			if i > 0 {
				writer.write(",")
			}
			writer.write(schemaKey(key))
			writer.write(":")
			writeSchema(writer, value[key], depth+1)
		}
		writer.write("}")
	default:
		writer.write("unknown")
	}
}

func validAudit(audit Audit) bool {
	return audit.Exists && (audit.Role == "Admin" || audit.Role == "User" || audit.Role == "Guest")
}

func duplicateJSONKey(data []byte) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var duplicate bool
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok {
					return errors.New("invalid object key")
				}
				if _, exists := seen[name]; exists {
					duplicate = true
				}
				seen[name] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid delimiter")
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') && closing != json.Delim(']') {
			return errors.New("invalid closing delimiter")
		}
		return nil
	}
	if err := walk(); err != nil {
		return false, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return false, errors.New("multiple JSON values")
		}
		return false, err
	}
	return duplicate, nil
}

func (p *Probe) reject(reason RejectReason) bool {
	p.candidate = ""
	clear(p.eligible)
	// IDs and early failures remain for late Abort/requestfailed correlation;
	// only eligible values and the candidate contain secrets.
	p.reason = reason
	p.state = Rejected
	return false
}

// AuditURL identifies the existing read-only ChatSvc member listing used for
// before/after comparison. The caller must keep the URL and identifiers out of logs.
func AuditURL(channelThreadID string) (string, error) {
	if !validSegment(channelThreadID) {
		return "", errors.New(string(RejectInvalidTarget))
	}
	return fmt.Sprintf("%s/api/chatsvc/amer/v1/threads/%s/members?view=msnp24Equivalent&pageSize=100&selectMemberRoles=Admin%%7CUser%%7CGuest", teamsOrigin, url.PathEscape(channelThreadID)), nil
}

// ParseMemberAudit extracts only existence and role for the in-memory target.
func ParseMemberAudit(body []byte, targetID string) (Audit, error) {
	if targetID == "" {
		return Audit{}, errors.New(string(RejectInvalidTarget))
	}
	duplicate, err := duplicateJSONKey(body)
	if err != nil {
		return Audit{}, err
	}
	if duplicate {
		return Audit{}, errors.New(string(RejectAmbiguousEvent))
	}
	envelope := make(map[string]json.RawMessage)
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Audit{}, err
	}
	membersJSON, exists := envelope["members"]
	if !exists {
		return Audit{}, errors.New(string(RejectAuditIncomplete))
	}
	var members []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(membersJSON, &members); err != nil {
		return Audit{}, err
	}
	var audit Audit
	for _, member := range members {
		if member.ID != targetID {
			continue
		}
		if audit.Exists {
			return Audit{}, errors.New(string(RejectAmbiguousEvent))
		}
		audit = Audit{Exists: true, Role: member.Role}
	}
	if !validAudit(audit) {
		return Audit{}, errors.New(string(RejectAuditIncomplete))
	}
	return audit, nil
}
