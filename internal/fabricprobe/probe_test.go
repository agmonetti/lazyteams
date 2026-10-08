package fabricprobe

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
		ID:            id,
		Method:        "POST",
		URL:           "https://teams.microsoft.com/fabric/amer/templates/api/teams/sensitive-team-id/channels/OID:sensitive-channel@tenant/users?forceSync=false",
		FrameURL:      "https://teams.microsoft.com/v2/",
		FrameRelation: "main",
		Body:          []byte(`{"users":[{"id":"OID:sensitive-user@tenant","role":2}]}`),
		Candidate:     candidate,
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
	if !probe.CloseContext() || !probe.AuditAfter(Audit{Exists: true, Role: "User"}) || !probe.Finalize(true) {
		t.Fatalf("finalize failed: %s/%s", probe.State(), probe.Reason())
	}
	if got := probe.Evidence(); got != (Evidence{State: WriteCandidateUnverified, Method: "POST", Route: "/fabric/amer/templates/api/teams/{team}/channels/{channel}/users", Attempts: 2, Aborted: true, RequestFailed: true, ContextClosed: true, AuditAfter: true, SnapshotUnchanged: true, Unchanged: true}) {
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

func TestEvidenceRequiresAuditAndSnapshotConfirmation(t *testing.T) {
	for _, method := range []string{"POST", "PATCH"} {
		for _, snapshotUnchanged := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/snapshot_unchanged=%t", method, snapshotUnchanged), func(t *testing.T) {
				probe := newReadyProbe(t)
				request := targetRequest("request", "secret-token")
				request.Method = method
				wantDecision := AbortTarget
				if method == "PATCH" {
					wantDecision = AbortUnknownMutation
				}
				if got := probe.Classify(request); got != wantDecision {
					t.Fatalf("Classify() = %v, want %v", got, wantDecision)
				}
				if !probe.Aborted(request.ID, nil) || !probe.RequestFailed(request.ID) || !probe.CloseContext() {
					t.Fatalf("setup failed: %s/%s", probe.State(), probe.Reason())
				}
				if got := probe.Evidence(); got.AuditAfter || got.SnapshotUnchanged || got.Unchanged {
					t.Fatalf("unverified checks announced before audit: %#v", got)
				}
				if !probe.AuditAfter(Audit{Exists: true, Role: "User"}) {
					t.Fatalf("audit failed: %s/%s", probe.State(), probe.Reason())
				}
				if got := probe.Evidence(); got.State != AuditAfterOK || !got.AuditAfter || got.SnapshotUnchanged || got.Unchanged {
					t.Fatalf("snapshot announced before comparison: %#v", got)
				}

				wantState, wantReason := WriteCandidateUnverified, RejectReason("")
				if method == "PATCH" {
					if probe.FinalizeDiagnostic(snapshotUnchanged) {
						t.Fatal("diagnostic mutation accepted")
					}
					wantState, wantReason = Rejected, RejectUnknownMutation
				} else if got := probe.Finalize(snapshotUnchanged); got != snapshotUnchanged {
					t.Fatalf("Finalize() = %v, want %v", got, snapshotUnchanged)
				}
				if !snapshotUnchanged {
					wantState, wantReason = Rejected, RejectSnapshotChanged
				}
				got := probe.Evidence()
				if got.State != wantState || got.ErrorCode != wantReason || !got.AuditAfter || got.SnapshotUnchanged != snapshotUnchanged || got.Unchanged != snapshotUnchanged {
					t.Fatalf("final evidence = %#v", got)
				}
			})
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
		"scheme":         func(r *Request) { r.URL = strings.Replace(r.URL, "https://", "http://", 1) },
		"host":           func(r *Request) { r.URL = strings.Replace(r.URL, "teams.microsoft.com", "evil.example", 1) },
		"port":           func(r *Request) { r.URL = strings.Replace(r.URL, "teams.microsoft.com", "teams.microsoft.com:443", 1) },
		"method":         func(r *Request) { r.Method = "PUT" },
		"path":           func(r *Request) { r.URL = strings.Replace(r.URL, "/users?", "/users/extra?", 1) },
		"query":          func(r *Request) { r.URL = strings.Replace(r.URL, "forceSync=false", "forceSync=true", 1) },
		"frame host":     func(r *Request) { r.FrameURL = "https://evil.example/" },
		"frame port":     func(r *Request) { r.FrameURL = "https://teams.microsoft.com:443/v2/" },
		"frame relation": func(r *Request) { r.FrameRelation = "indeterminate" },
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

func TestUnknownMutationProducesSanitizedFingerprint(t *testing.T) {
	probe := newReadyProbe(t)
	request := targetRequest("unknown", "secret-token")
	request.Method = "PATCH"
	request.URL = "https://teams.microsoft.com/fabric/amer/templates/api/teams/real-team/channels/real-channel/users?forceSync=false&user=secret"
	request.FrameURL = "https://evil.example/frame"
	request.ContentType = "application/json; charset=utf-8"
	request.Body = []byte(`{"users":[{"id":"secret-user","role":2}],"note":"secret"}`)
	if got := probe.Classify(request); got != AbortUnknownMutation {
		t.Fatalf("Classify() = %v, want AbortUnknownMutation", got)
	}
	got := probe.Evidence().Mutation
	if got == nil {
		t.Fatal("missing mutation fingerprint")
	}
	want := &RequestFingerprint{
		Method:           "PATCH",
		Route:            "/fabric/amer/templates/api/teams/{param}/channels/{param}/users",
		Query:            "forceSync",
		ContentType:      "application/json",
		BodySchema:       "object{note:string,users:array[1]{object{id:string,role:number}}}",
		Frame:            "other_origin",
		FrameRelation:    "main",
		SchemeHTTPS:      true,
		HostClass:        "teams.microsoft.com",
		HostMatch:        true,
		PathMatch:        false,
		PortEmpty:        true,
		UserinfoAbsent:   true,
		FragmentAbsent:   true,
		QueryMatch:       false,
		PayloadMatch:     false,
		CandidatePresent: true,
	}
	if !reflect.DeepEqual(*got, *want) {
		t.Fatalf("fingerprint = %#v, want %#v", *got, *want)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-token", "real-team", "real-channel", "secret-user", "secret"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("fingerprint leaked %q: %s", secret, encoded)
		}
	}
}

func TestFingerprintReportsExactMatchPredicatesAndFrameLineage(t *testing.T) {
	probe := newReadyProbe(t)
	request := targetRequest("unknown", "secret-token")
	request.Method = "PATCH"
	request.FrameURL = "https://teams.cloud.microsoft/app"
	request.FrameRelation = "child"
	request.FrameAncestors = []string{"https://teams.microsoft.com/v2/", "about:blank", "https://evil.example/ignored"}
	if got := probe.Classify(request); got != AbortUnknownMutation {
		t.Fatalf("Classify() = %v, want AbortUnknownMutation", got)
	}
	mutation := probe.Evidence().Mutation
	if mutation == nil || !mutation.SchemeHTTPS || mutation.HostClass != "teams.microsoft.com" || !mutation.HostMatch || !mutation.PathMatch || !mutation.PortEmpty || !mutation.UserinfoAbsent || !mutation.FragmentAbsent || !mutation.QueryMatch || !mutation.PayloadMatch || !mutation.CandidatePresent {
		t.Fatalf("match predicates = %#v, want endpoint/query/payload/candidate true", mutation)
	}
	if mutation.Frame != "teams.cloud.microsoft" || mutation.FrameRelation != "child" {
		t.Fatalf("frame classification = %#v", mutation)
	}
	wantAncestors := []string{"teams.microsoft.com", "opaque_or_missing", "other_origin"}
	if !reflect.DeepEqual(mutation.FrameAncestors, wantAncestors) {
		t.Fatalf("frame ancestors = %#v, want %#v", mutation.FrameAncestors, wantAncestors)
	}
}

func TestFrameClassificationSeparatesOriginFromLineage(t *testing.T) {
	tests := []struct {
		name       string
		frame      string
		relation   string
		ancestors  []string
		wantFrame  string
		wantRel    string
		wantParent int
	}{
		{"official main", "https://teams.microsoft.com/v2/", "main", nil, "teams.microsoft.com", "main", 0},
		{"cloud child", "https://teams.cloud.microsoft/app", "child", []string{"https://teams.microsoft.com/v2/"}, "teams.cloud.microsoft", "child", 1},
		{"other main", "https://evil.example/frame", "main", nil, "other_origin", "main", 0},
		{"opaque main", "", "main", nil, "opaque_or_missing", "main", 0},
		{"opaque child", "", "child", []string{"about:blank"}, "opaque_or_missing", "child", 1},
		{"indeterminate", "", "", nil, "opaque_or_missing", "indeterminate", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFrame := frameClass(tt.frame)
			if gotFrame != tt.wantFrame {
				t.Fatalf("frameClass() = %q, want %q", gotFrame, tt.wantFrame)
			}
			if got := frameRelation(tt.relation); got != tt.wantRel {
				t.Fatalf("frameRelation() = %q, want %q", got, tt.wantRel)
			}
			if got := len(ancestorClasses(tt.ancestors)); got != tt.wantParent {
				t.Fatalf("ancestor count = %d, want %d", got, tt.wantParent)
			}
		})
	}
}

func TestFrameAncestorsAreBoundedAndSanitized(t *testing.T) {
	ancestors := []string{
		"https://teams.microsoft.com/one",
		"https://teams.cloud.microsoft/two",
		"about:blank",
		"https://evil.example/three",
		"https://teams.microsoft.com/five",
	}
	got := ancestorClasses(ancestors)
	want := []string{"teams.microsoft.com", "teams.cloud.microsoft", "opaque_or_missing", "other_origin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ancestorClasses() = %#v, want %#v", got, want)
	}
	if len(got) > 4 {
		t.Fatalf("ancestor classes = %d, want at most 4", len(got))
	}
}

func TestSchemaSanitizesDynamicKeysAndDescribesEveryArrayItem(t *testing.T) {
	body := []byte(`{"users":[{"id":"u1","role":2},{"id":"u2","role":"unexpected"}],"secret-id":"leak","nested":[[],[true,false]]}`)
	got := bodySchema(body)
	want := "object{<key>:array[2]{array[0]{},array[2]{bool,bool}},<key>:string,users:array[2]{object{id:string,role:number},object{id:string,role:string}}}"
	if got != want {
		t.Fatalf("bodySchema() = %q, want %q", got, want)
	}
	for _, secret := range []string{"secret-id", "leak", "u1", "u2"} {
		if strings.Contains(got, secret) {
			t.Fatalf("schema leaked %q: %s", secret, got)
		}
	}
}

func TestSchemaBoundsLargeArraysWithoutLosingCardinality(t *testing.T) {
	items := make([]string, maxArrayItems+1)
	for i := range items {
		items[i] = "1"
	}
	body := []byte(`{"users":[` + strings.Join(items, ",") + `]}`)
	got := bodySchema(body)
	if !strings.Contains(got, "users:array[17]{") || !strings.Contains(got, "<more>}}") {
		t.Fatalf("bodySchema() did not preserve bounded cardinality: %q", got)
	}
}

func TestBodySchemaRejectsUnboundedInputs(t *testing.T) {
	t.Run("body bytes", func(t *testing.T) {
		body := []byte(`{"note":"` + strings.Repeat("x", maxBodyBytes) + `"}`)
		if got := bodySchema(body); got != "body_too_large" {
			t.Fatalf("bodySchema() = %q, want body_too_large", got)
		}
	})
	t.Run("wide object", func(t *testing.T) {
		fields := make([]string, maxObjectFields+1)
		for i := range fields {
			fields[i] = fmt.Sprintf(`"field%d":%d`, i, i)
		}
		got := bodySchema([]byte("{" + strings.Join(fields, ",") + "}"))
		if !strings.HasPrefix(got, "object[33]{<more>}") {
			t.Fatalf("bodySchema() = %q, want bounded wide-object marker", got)
		}
		if len(got) > maxSchemaBytes {
			t.Fatalf("schema length = %d, want <= %d", len(got), maxSchemaBytes)
		}
	})
	t.Run("duplicate keys", func(t *testing.T) {
		if got := bodySchema([]byte(`{"role":1,"role":2}`)); got != "duplicate_keys" {
			t.Fatalf("bodySchema() = %q, want duplicate_keys", got)
		}
	})
}

func TestFingerprintLimitsAreEnforcedEndToEnd(t *testing.T) {
	arrayOf := func(item string, count int) string {
		items := make([]string, count)
		for i := range items {
			items[i] = item
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	stringArray := arrayOf(`"secret"`, maxArrayItems)
	largeSchema := arrayOf(arrayOf(`{"note":"secret"}`, maxArrayItems), maxArrayItems)
	budgetSchema := arrayOf(stringArray, maxArrayItems)
	wideFields := make([]string, maxObjectFields+1)
	for i := range wideFields {
		wideFields[i] = fmt.Sprintf(`"secret-field-%d":"secret-value"`, i)
	}
	deep := `"secret"`
	for range maxSchemaDepth + 1 {
		deep = "[" + deep + "]"
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		marker string
	}{
		{name: "body too large", method: "PATCH", path: "/fabric/write", body: `{"note":"` + strings.Repeat("secret", maxBodyBytes) + `"}`, marker: "body_too_large"},
		{name: "wide object", method: "PATCH", path: "/fabric/write", body: "{" + strings.Join(wideFields, ",") + "}", marker: "object[33]{<more>}"},
		{name: "depth", method: "PATCH", path: "/fabric/write", body: deep, marker: "<depth>"},
		{name: "duplicate keys", method: "PATCH", path: "/fabric/write", body: `{"role":1,"role":2,"note":"secret"}`, marker: "duplicate_keys"},
		{name: "invalid json", method: "PATCH", path: "/fabric/write", body: `{"note":"secret"`, marker: "invalid_json"},
		{name: "schema too large", method: "PATCH", path: "/fabric/write", body: largeSchema, marker: "schema_too_large"},
		{name: "global fingerprint budget", method: "PATCH", path: strings.Repeat("/fabric", 50), body: budgetSchema, marker: "fingerprint_too_large"},
		{name: "method and route", method: strings.Repeat("SECRET", 100), path: "/fabric/" + strings.Repeat("secret/", maxRouteInputBytes), body: `{}`, marker: "route_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			probe := newReadyProbe(t)
			request := targetRequest("unknown", "secret-token")
			request.Method = test.method
			request.URL = "https://teams.microsoft.com" + test.path
			request.ContentType = "application/json"
			request.Body = []byte(test.body)
			if got := probe.Classify(request); got != AbortUnknownMutation {
				t.Fatalf("Classify() = %v, want AbortUnknownMutation", got)
			}
			encoded, err := json.Marshal(probe.Evidence())
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded)+1 > maxEvidenceJSONBytes {
				t.Fatalf("JSONL length = %d, want <= %d: %s", len(encoded)+1, maxEvidenceJSONBytes, encoded)
			}
			var evidence Evidence
			if err := json.Unmarshal(encoded, &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.Mutation == nil || (!strings.Contains(evidence.Mutation.BodySchema, test.marker) && evidence.Mutation.Route != test.marker) {
				t.Fatalf("evidence missing %q: %s", test.marker, encoded)
			}
			for _, secret := range []string{"secret-token", "secret-field", "secret-value", "SECRETSECRET", "secret"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("evidence leaked %q: %s", secret, encoded)
				}
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

func TestAmbiguousMutationWithoutDistinctRequestIDCannotConfirmAborts(t *testing.T) {
	for _, id := range []string{"", "first"} {
		probe := newReadyProbe(t)
		if probe.Classify(targetRequest("first", "token")) != AbortTarget || !probe.Aborted("first", nil) || !probe.RequestFailed("first") {
			t.Fatal("first mutation did not complete")
		}
		unknown := targetRequest(id, "token")
		unknown.Method = "PATCH"
		if probe.Classify(unknown) != AbortUnknownMutation || probe.Reason() != RejectAmbiguousEvent || probe.Evidence().Aborted {
			t.Fatalf("unidentifiable second mutation claimed a complete abort: %#v", probe.Evidence())
		}
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

func TestSecondUnknownMutationInvalidatesDiagnostic(t *testing.T) {
	probe := newReadyProbe(t)
	request1 := targetRequest("unknown1", "secret-token")
	request1.Method = "PATCH"
	if got := probe.Classify(request1); got != AbortUnknownMutation {
		t.Fatalf("Classify(1) = %v, want AbortUnknownMutation", got)
	}
	if probe.State() != Rejected || probe.Reason() != RejectUnknownMutation {
		t.Fatalf("state = %v, reason = %v after first unknown mutation", probe.State(), probe.Reason())
	}

	request2 := targetRequest("unknown2", "secret-token")
	request2.Method = "PATCH"
	if got := probe.Classify(request2); got != AbortUnknownMutation {
		t.Fatalf("Classify(2) = %v, want AbortUnknownMutation", got)
	}
	if probe.State() != Rejected || probe.Reason() != RejectAmbiguousEvent {
		t.Fatalf("state = %v, reason = %v after second unknown mutation", probe.State(), probe.Reason())
	}
}

func TestResponseObservedSetsResponseObservedInEvidence(t *testing.T) {
	probe := newReadyProbe(t)
	request1 := targetRequest("unknown1", "secret-token")
	request1.Method = "PATCH"
	probe.Classify(request1)
	probe.Response("unknown1")
	ev := probe.Evidence()
	if !ev.ResponseObserved {
		t.Fatal("expected ResponseObserved to be true")
	}
}

func TestUnknownMutationRequestFailedBeforeAbort(t *testing.T) {
	probe := newReadyProbe(t)
	request1 := targetRequest("unknown1", "secret-token")
	request1.Method = "PATCH"
	probe.Classify(request1)

	// requestFailed arrives BEFORE Aborted
	if !probe.RequestFailed("unknown1") {
		t.Fatal("RequestFailed rejected")
	}

	if !probe.Aborted("unknown1", nil) {
		t.Fatal("Aborted rejected")
	}

	if !probe.failed || !probe.aborted {
		t.Fatalf("expected failed=true aborted=true, got failed=%v aborted=%v", probe.failed, probe.aborted)
	}
}
