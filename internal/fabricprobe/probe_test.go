package fabricprobe

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var testTarget = Target{Team: "sensitive-team-id", Channel: "OID:sensitive-channel@tenant", User: "OID:sensitive-user@tenant", Role: 2}

func newReadyProbe(t *testing.T) *Probe {
	t.Helper()
	probe, err := New(testTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !probe.InstallRoute() || !probe.AuditBefore(Audit{Exists: true, Role: "User"}) {
		t.Fatalf("setup failed: %s/%s", probe.State(), probe.Reason())
	}
	return probe
}

func targetRequest(id, candidate string) Request {
	return Request{
		ID:        id,
		Method:    "POST",
		URL:       "https://teams.microsoft.com/fabric/amer/templates/api/teams/sensitive-team-id/channels/OID:sensitive-channel@tenant/users?forceSync=false",
		FrameURL:  "https://teams.microsoft.com/v2/",
		Body:      []byte(`{"users":[{"id":"OID:sensitive-user@tenant","role":2}]}`),
		Candidate: candidate,
	}
}

func TestProbeCompletesOnlyAfterAbortFailureAndUnchangedAudit(t *testing.T) {
	probe := newReadyProbe(t)
	for _, attempt := range []string{"first", "retry"} {
		request := targetRequest(attempt, "secret-token")
		if got := probe.Classify(request); got != AbortTarget {
			t.Fatalf("Classify() = %v, want AbortTarget (%s)", got, probe.Reason())
		}
		if !probe.Aborted(attempt, nil) || !probe.RequestFailed(attempt) {
			t.Fatalf("attempt failed: %s/%s", probe.State(), probe.Reason())
		}
	}
	if !probe.CloseContext() || !probe.AuditAfter(Audit{Exists: true, Role: "User"}) || !probe.Finalize() {
		t.Fatalf("finalize failed: %s/%s", probe.State(), probe.Reason())
	}
	if got := probe.Evidence(); got != (Evidence{State: WriteCandidateUnverified, Method: "POST", Route: "/fabric/amer/templates/api/teams/{team}/channels/{channel}/users", Attempts: 2, Unchanged: true}) {
		t.Fatalf("Evidence() = %#v", got)
	}

	encoded, err := json.Marshal(probe.Evidence())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-token", testTarget.Team, testTarget.Channel, testTarget.User} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("sanitized evidence leaked %q: %s", secret, encoded)
		}
	}
}

func TestProbeCorrelatesOverlappingRequests(t *testing.T) {
	probe := newReadyProbe(t)
	if probe.Classify(targetRequest("first", "token")) != AbortTarget || probe.Classify(targetRequest("second", "token")) != AbortTarget {
		t.Fatalf("overlapping requests were not both selected: %s/%s", probe.State(), probe.Reason())
	}
	if !probe.Aborted("second", nil) || !probe.Aborted("first", nil) || !probe.RequestFailed("first") || !probe.RequestFailed("second") {
		t.Fatalf("out-of-order correlation failed: %s/%s", probe.State(), probe.Reason())
	}
	if !probe.CloseContext() {
		t.Fatalf("close failed: %s/%s", probe.State(), probe.Reason())
	}
}

func TestProbeCorrelatesRequestFailedBeforeAbortReturns(t *testing.T) {
	probe := newReadyProbe(t)
	if probe.Classify(targetRequest("request", "token")) != AbortTarget || !probe.RequestFailed("request") || !probe.Aborted("request", nil) {
		t.Fatalf("early requestfailed correlation failed: %s/%s", probe.State(), probe.Reason())
	}
	if probe.State() != RequestFailedConfirmed || !probe.CloseContext() {
		t.Fatalf("early requestfailed did not confirm abort: %s/%s", probe.State(), probe.Reason())
	}
}

func TestMatcherFailsClosedForEveryTargetMismatch(t *testing.T) {
	tests := map[string]func(*Request){
		"scheme":     func(r *Request) { r.URL = strings.Replace(r.URL, "https://", "http://", 1) },
		"host":       func(r *Request) { r.URL = strings.Replace(r.URL, "teams.microsoft.com", "evil.example", 1) },
		"port":       func(r *Request) { r.URL = strings.Replace(r.URL, "teams.microsoft.com", "teams.microsoft.com:443", 1) },
		"method":     func(r *Request) { r.Method = "PUT" },
		"path":       func(r *Request) { r.URL = strings.Replace(r.URL, "/users?", "/users/extra?", 1) },
		"query":      func(r *Request) { r.URL = strings.Replace(r.URL, "forceSync=false", "forceSync=true", 1) },
		"frame host": func(r *Request) { r.FrameURL = "https://evil.example/" },
		"frame port": func(r *Request) { r.FrameURL = "https://teams.microsoft.com:443/v2/" },
		"extra field": func(r *Request) {
			r.Body = []byte(`{"users":[{"id":"OID:sensitive-user@tenant","role":2,"extra":true}]}`)
		},
		"two users": func(r *Request) {
			r.Body = []byte(`{"users":[{"id":"OID:sensitive-user@tenant","role":2},{"id":"other","role":2}]}`)
		},
		"wrong user":    func(r *Request) { r.Body = []byte(`{"users":[{"id":"other","role":2}]}`) },
		"wrong role":    func(r *Request) { r.Body = []byte(`{"users":[{"id":"OID:sensitive-user@tenant","role":1}]}`) },
		"trailing json": func(r *Request) { r.Body = append(r.Body, []byte(` {}`)...) },
		"no candidate":  func(r *Request) { r.Candidate = "" },
		"no request id": func(r *Request) { r.ID = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			probe := newReadyProbe(t)
			request := targetRequest("request", "secret-token")
			mutate(&request)
			if got := probe.Classify(request); got != AbortUnknownMutation {
				t.Fatalf("Classify() = %v, want AbortUnknownMutation", got)
			}
			if probe.State() != Rejected {
				t.Fatalf("state = %s, want rejected", probe.State())
			}
		})
	}
}

func TestRouterAllowsOnlyNonFabricOrIdempotentFabric(t *testing.T) {
	probe := newReadyProbe(t)
	for _, request := range []Request{
		{Method: "POST", URL: "https://teams.microsoft.com/api/other"},
		{Method: "GET", URL: "https://teams.microsoft.com/fabric/read"},
		{Method: "HEAD", URL: "https://teams.microsoft.com/fabric/read"},
		{Method: "OPTIONS", URL: "https://teams.microsoft.com/fabric/read"},
	} {
		if got := probe.Classify(request); got != Continue {
			t.Fatalf("Classify(%s %s) = %v", request.Method, request.URL, got)
		}
	}
	if probe.State() != AuditBeforeOK {
		t.Fatalf("safe requests changed state to %s", probe.State())
	}

	if got := probe.Classify(Request{Method: "DELETE", URL: "https://other.example/fabric/write"}); got != AbortUnknownMutation {
		t.Fatalf("unknown Fabric mutation = %v", got)
	}
	if probe.Reason() != RejectUnknownMutation {
		t.Fatalf("reason = %s", probe.Reason())
	}
}

func TestProbeRejectsUnsafeLifecycleEvents(t *testing.T) {
	t.Run("abort error", func(t *testing.T) {
		probe := newReadyProbe(t)
		probe.Classify(targetRequest("request", "token"))
		probe.Aborted("request", errors.New("abort failed"))
		if probe.Reason() != RejectAbortFailed {
			t.Fatalf("reason = %s", probe.Reason())
		}
	})
	t.Run("response", func(t *testing.T) {
		probe := newReadyProbe(t)
		probe.Classify(targetRequest("request", "token"))
		probe.Response("request")
		if probe.Reason() != RejectResponseObserved {
			t.Fatalf("reason = %s", probe.Reason())
		}
	})
	t.Run("changed candidate", func(t *testing.T) {
		probe := newReadyProbe(t)
		probe.Classify(targetRequest("first", "token-a"))
		probe.Aborted("first", nil)
		probe.RequestFailed("first")
		probe.Classify(targetRequest("retry", "token-b"))
		probe.Aborted("retry", nil)
		if probe.Reason() != RejectCandidateChanged {
			t.Fatalf("reason = %s", probe.Reason())
		}
	})
	t.Run("audit changed", func(t *testing.T) {
		probe := newReadyProbe(t)
		probe.Classify(targetRequest("request", "token"))
		probe.Aborted("request", nil)
		probe.RequestFailed("request")
		probe.CloseContext()
		probe.AuditAfter(Audit{Exists: true, Role: "Admin"})
		if probe.Reason() != RejectAuditChanged {
			t.Fatalf("reason = %s", probe.Reason())
		}
	})
	t.Run("cancel", func(t *testing.T) {
		probe := newReadyProbe(t)
		probe.Cancel()
		if probe.Reason() != RejectCancelled {
			t.Fatalf("reason = %s", probe.Reason())
		}
	})
	t.Run("timeout", func(t *testing.T) {
		probe := newReadyProbe(t)
		probe.Timeout()
		if probe.Reason() != RejectTimeout {
			t.Fatalf("reason = %s", probe.Reason())
		}
	})
}

func TestAuditMustIdentifyExistingMemberAndKnownRole(t *testing.T) {
	for name, audit := range map[string]Audit{
		"missing":      {},
		"missing role": {Exists: true},
		"unknown role": {Exists: true, Role: "Owner"},
	} {
		t.Run(name, func(t *testing.T) {
			probe, err := New(testTarget)
			if err != nil {
				t.Fatal(err)
			}
			probe.InstallRoute()
			if probe.AuditBefore(audit) || probe.Reason() != RejectAuditIncomplete {
				t.Fatalf("audit accepted: %s/%s", probe.State(), probe.Reason())
			}
		})
	}
}

func TestReadOnlyMemberAudit(t *testing.T) {
	auditURL, err := AuditURL("19:private@thread.tacv2")
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "https://teams.microsoft.com/api/chatsvc/amer/v1/threads/19:private@thread.tacv2/members?view=msnp24Equivalent&pageSize=100&selectMemberRoles=Admin%7CUser%7CGuest"
	if auditURL != wantURL {
		t.Fatalf("AuditURL() = %q", auditURL)
	}

	body := []byte(`{"members":[{"id":"8:orgid:target","role":"Admin","displayName":"secret"},{"id":"other","role":"User"}]}`)
	audit, err := ParseMemberAudit(body, "8:orgid:target")
	if err != nil || audit != (Audit{Exists: true, Role: "Admin"}) {
		t.Fatalf("ParseMemberAudit() = %#v, %v", audit, err)
	}
	if _, err := ParseMemberAudit(body, "missing"); err == nil {
		t.Fatal("missing target must reject an inconclusive audit")
	}
	if _, err := ParseMemberAudit([]byte(`{"members":[{"id":"same","role":"Admin"},{"id":"same","role":"User"}]}`), "same"); err == nil {
		t.Fatal("duplicate target must be ambiguous")
	}
	if _, err := ParseMemberAudit([]byte(`{"members":[{"id":"same","id":"same","role":"Admin"}]}`), "same"); err == nil {
		t.Fatal("duplicate JSON keys must be ambiguous")
	}
	if _, err := ParseMemberAudit([]byte(`{"value":[]}`), "same"); err == nil {
		t.Fatal("missing members field must reject an inconclusive audit")
	}
}
