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
	ID        string
	Method    string
	URL       string
	FrameURL  string
	Body      []byte
	Candidate string
}

type Audit struct {
	Exists bool
	Role   string
}

type Evidence struct {
	State     State        `json:"state"`
	Method    string       `json:"method,omitempty"`
	Route     string       `json:"route,omitempty"`
	Attempts  int          `json:"attempts"`
	Unchanged bool         `json:"unchanged"`
	ErrorCode RejectReason `json:"error_code,omitempty"`
}

type Probe struct {
	mu        sync.Mutex
	target    Target
	state     State
	reason    RejectReason
	before    Audit
	candidate string
	eligible   map[string]string
	failedEarly map[string]bool
	attempts   map[string]bool
}

func New(target Target) (*Probe, error) {
	if !validSegment(target.Team) || !validSegment(target.Channel) || !validSegment(target.User) || (target.Role != 1 && target.Role != 2) {
		return nil, errors.New(string(RejectInvalidTarget))
	}
	return &Probe{target: target, state: Prepared, eligible: make(map[string]string), failedEarly: make(map[string]bool), attempts: make(map[string]bool)}, nil
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
	if p.state == Rejected {
		return AbortUnknownMutation
	}
	if p.state != AuditBeforeOK && p.state != TargetAborted && p.state != RequestFailedConfirmed {
		p.reject(RejectInvalidOrder)
		return AbortUnknownMutation
	}
	if !p.matchesTarget(req, u) || req.ID == "" {
		p.reject(RejectUnknownMutation)
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
	if req.Method != http.MethodPost || u.Scheme != "https" || u.Host != "teams.microsoft.com" || u.Port() != "" || u.User != nil || u.Fragment != "" || u.RawQuery != "forceSync=false" || req.Candidate == "" {
		return false
	}
	frame, err := url.Parse(req.FrameURL)
	if err != nil || frame.Scheme != "https" || frame.Host != "teams.microsoft.com" || frame.Port() != "" || frame.User != nil {
		return false
	}
	expectedPath := "/fabric/amer/templates/api/teams/" + url.PathEscape(p.target.Team) + "/channels/" + url.PathEscape(p.target.Channel) + "/users"
	if u.EscapedPath() != expectedPath {
		return false
	}
	var payload struct {
		Users []struct {
			ID   string `json:"id"`
			Role int    `json:"role"`
		} `json:"users"`
	}
	if duplicateJSONKey(req.Body) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(req.Body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return false
	}
	return len(payload.Users) == 1 && payload.Users[0].ID == p.target.User && payload.Users[0].Role == p.target.Role
}

// Aborted records a successful Route.Abort for a request previously matched
// by Classify. The candidate never leaves the probe.
func (p *Probe) Aborted(requestID string, abortErr error) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
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
	if p.state == Rejected {
		return false
	}
	failed, aborted := p.attempts[requestID]
	if aborted {
		if failed {
			return p.reject(RejectAmbiguousEvent)
		}
		p.attempts[requestID] = true
		p.state = RequestFailedConfirmed
		return true
	}
	if _, eligible := p.eligible[requestID]; eligible && !p.failedEarly[requestID] {
		p.failedEarly[requestID] = true
		return true
	}
	return p.reject(RejectAmbiguousEvent)
}

func (p *Probe) Response(requestID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, aborted := p.attempts[requestID]
	_, eligible := p.eligible[requestID]
	if aborted || eligible {
		return p.reject(RejectResponseObserved)
	}
	return true
}

func (p *Probe) CloseContext() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != RequestFailedConfirmed || len(p.attempts) == 0 || len(p.eligible) != 0 {
		return p.reject(RejectInvalidOrder)
	}
	for _, failed := range p.attempts {
		if !failed {
			return p.reject(RejectAmbiguousEvent)
		}
	}
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
	p.state = AuditAfterOK
	return true
}

func (p *Probe) Finalize() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != AuditAfterOK {
		return p.reject(RejectInvalidOrder)
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
	evidence := Evidence{State: p.state, Attempts: len(p.attempts), Unchanged: p.state == AuditAfterOK || p.state == WriteCandidateUnverified, ErrorCode: p.reason}
	if len(p.attempts) > 0 {
		evidence.Method = http.MethodPost
		evidence.Route = "/fabric/amer/templates/api/teams/{team}/channels/{channel}/users"
	}
	return evidence
}

func validAudit(audit Audit) bool {
	return audit.Exists && (audit.Role == "Admin" || audit.Role == "User" || audit.Role == "Guest")
}

func duplicateJSONKey(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() bool
	walk = func() bool {
		token, err := decoder.Token()
		if err != nil {
			return true
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return false
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return true
				}
				name, ok := key.(string)
				if !ok {
					return true
				}
				if _, exists := seen[name]; exists {
					return true
				}
				seen[name] = struct{}{}
				if walk() {
					return true
				}
			}
		case '[':
			for decoder.More() {
				if walk() {
					return true
				}
			}
		default:
			return true
		}
		_, err = decoder.Token()
		return err != nil
	}
	return walk()
}

func (p *Probe) reject(reason RejectReason) bool {
	p.candidate = ""
	clear(p.eligible)
	clear(p.failedEarly)
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
	if duplicateJSONKey(body) {
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
