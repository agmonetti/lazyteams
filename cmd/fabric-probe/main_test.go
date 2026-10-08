package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/fabricprobe"
	"github.com/agmonetti/lazyteams/internal/helpers"
	"github.com/mxschmitt/playwright-go"
)

var probeTarget = fabricprobe.Target{Team: "team-id", Channel: "OID:channel@tenant", User: "OID:user@tenant", Role: 2}

func configuredRequest() fabricprobe.Request {
	return fabricprobe.Request{
		Method:        "POST",
		URL:           "https://teams.microsoft.com/fabric/amer/templates/api/teams/team-id/channels/OID:channel@tenant/users?forceSync=false",
		FrameURL:      "https://teams.microsoft.com/v2/",
		FrameRelation: "main",
		Body:          []byte(`{"users":[{"id":"OID:user@tenant","role":2}]}`),
		Candidate:     "secret-candidate",
	}
}

func runTestLifecycle(probe *fabricprobe.Probe, wait func() error) (err error, audits, closes, snapshots int) {
	err = runLifecycle(context.Background(), probe, lifecycleEdges{
		installRoute: func() error { return nil },
		goOnline:     func() error { return nil },
		audit: func(context.Context) (fabricprobe.Audit, error) {
			audits++
			return fabricprobe.Audit{Exists: true, Role: "User"}, nil
		},
		navigate:      func() error { return nil },
		waitForTarget: func(context.Context) error { return wait() },
		closeContext:  func() error { closes++; return nil },
		snapshotUnchanged: func() bool {
			snapshots++
			return true
		},
	})
	return
}

type testFrame struct {
	url    string
	parent *testFrame
}

func (f *testFrame) URL() string { return f.url }
func (f *testFrame) Parent() frameReader {
	if f.parent == nil {
		return nil
	}
	return f.parent
}

func TestFrameInfoDistinguishesOpaqueMainFromIndeterminateAndBoundsAncestors(t *testing.T) {
	fourth := &testFrame{url: "https://four.example"}
	third := &testFrame{url: "https://three.example", parent: fourth}
	second := &testFrame{url: "https://two.example", parent: third}
	first := &testFrame{url: "https://one.example", parent: second}
	tests := []struct {
		name          string
		load          func() frameReader
		wantURL       string
		wantAncestors []string
		wantRelation  string
	}{
		{"opaque main", func() frameReader { return &testFrame{} }, "", nil, "main"},
		{"child", func() frameReader { return &testFrame{url: "https://teams.cloud.microsoft", parent: first} }, "https://teams.cloud.microsoft", []string{"https://one.example", "https://two.example", "https://three.example", "https://four.example"}, "child"},
		{"nil", func() frameReader { return nil }, "", nil, "indeterminate"},
		{"panic", func() frameReader { panic("frame unavailable") }, "", nil, "indeterminate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ancestors, relation := frameInfo(tt.load)
			if url != tt.wantURL || !reflect.DeepEqual(ancestors, tt.wantAncestors) || relation != tt.wantRelation {
				t.Fatalf("frameInfo() = %q, %#v, %q; want %q, %#v, %q", url, ancestors, relation, tt.wantURL, tt.wantAncestors, tt.wantRelation)
			}
		})
	}
}

func TestLifecycleInstallsRouteBeforeNavigationAndClosesBeforeFinalAudit(t *testing.T) {
	probe, err := fabricprobe.New(probeTarget)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	tracker := newRequestTracker()
	edges := lifecycleEdges{
		installRoute: func() error { order = append(order, "route"); return nil },
		goOnline:     func() error { order = append(order, "online"); return nil },
		audit: func(context.Context) (fabricprobe.Audit, error) {
			order = append(order, "audit")
			return fabricprobe.Audit{Exists: true, Role: "User"}, nil
		},
		navigate: func() error { order = append(order, "navigate"); return nil },
		waitForTarget: func(context.Context) error {
			order = append(order, "wait")
			dispatchRoute(probe, tracker, "request", configuredRequest(), func() error {
				requestFailed(probe, tracker, "request")
				return nil
			}, func() error { return errors.New("target must not continue") })
			return nil
		},
		closeContext: func() error { order = append(order, "close"); return nil },
		snapshotUnchanged: func() bool {
			order = append(order, "snapshot")
			got := probe.Evidence()
			if got.State != fabricprobe.AuditAfterOK || !got.AuditAfter || got.SnapshotUnchanged || got.Unchanged {
				t.Fatalf("legacy evidence before snapshot comparison: %#v", got)
			}
			return true
		},
	}
	if err := runLifecycle(context.Background(), probe, edges); err != nil {
		t.Fatal(err)
	}
	want := []string{"route", "online", "audit", "navigate", "wait", "close", "audit", "snapshot"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if probe.State() != fabricprobe.WriteCandidateUnverified {
		t.Fatalf("state = %s", probe.State())
	}
	encoded, err := json.Marshal(probe.Evidence())
	if err != nil {
		t.Fatal(err)
	}
	wantEvidence := `{"state":"write_candidate_unverified","method":"POST","route":"/fabric/amer/templates/api/teams/{team}/channels/{channel}/users","attempts":1,"aborted":true,"request_failed":true,"response_observed":false,"context_closed":true,"audit_after":true,"snapshot_unchanged":true,"unchanged":true}`
	if string(encoded) != wantEvidence {
		t.Fatalf("evidence JSON = %s, want %s", encoded, wantEvidence)
	}
}

func TestRouteAdapterAbortsUnknownMutationAndRejectsResponse(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	probe.InstallRoute()
	probe.AuditBefore(fabricprobe.Audit{Exists: true, Role: "User"})
	tracker := newRequestTracker()
	aborted, continued := 0, 0
	unknown := configuredRequest()
	unknown.URL = "https://teams.microsoft.com/fabric/unknown"
	dispatchRoute(probe, tracker, "unknown", unknown, func() error { aborted++; return nil }, func() error { continued++; return nil })
	if aborted != 1 || continued != 0 || probe.Reason() != fabricprobe.RejectUnknownMutation {
		t.Fatalf("aborted=%d continued=%d state=%s reason=%s", aborted, continued, probe.State(), probe.Reason())
	}

	probe, _ = fabricprobe.New(probeTarget)
	probe.InstallRoute()
	probe.AuditBefore(fabricprobe.Audit{Exists: true, Role: "User"})
	tracker = newRequestTracker()
	dispatchRoute(probe, tracker, "target", configuredRequest(), func() error {
		responseObserved(probe, tracker, "target")
		return nil
	}, func() error { return nil })
	if probe.Reason() != fabricprobe.RejectResponseObserved {
		t.Fatalf("response reason = %s", probe.Reason())
	}
}

func TestLifecycleCancellationClosesContext(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closed := 0
	err := runLifecycle(ctx, probe, lifecycleEdges{
		installRoute: func() error { return nil },
		goOnline:     func() error { return nil },
		audit: func(context.Context) (fabricprobe.Audit, error) {
			return fabricprobe.Audit{Exists: true, Role: "User"}, nil
		},
		navigate:          func() error { return nil },
		waitForTarget:     func(context.Context) error { return ctx.Err() },
		closeContext:      func() error { closed++; return nil },
		snapshotUnchanged: func() bool { return true },
	})
	if err == nil || probe.Reason() != fabricprobe.RejectCancelled || closed != 1 {
		t.Fatalf("err=%v reason=%s closed=%d", err, probe.Reason(), closed)
	}
}

func TestLifecycleCompletesUnknownMutationDiagnosticsBeforeRejectingAndSanitizesStderr(t *testing.T) {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		w.Close()
	}()

	probe, _ := fabricprobe.New(probeTarget)
	tracker := newRequestTracker()
	order := []string{}
	err := runLifecycle(context.Background(), probe, lifecycleEdges{
		installRoute: func() error { order = append(order, "route"); return nil },
		goOnline:     func() error { order = append(order, "online"); return nil },
		audit: func(context.Context) (fabricprobe.Audit, error) {
			order = append(order, "audit")
			return fabricprobe.Audit{Exists: true, Role: "User"}, nil
		},
		navigate: func() error { order = append(order, "navigate"); return nil },
		waitForTarget: func(context.Context) error {
			order = append(order, "wait")
			unknown := configuredRequest()
			unknown.Method = "PATCH"
			dispatchRoute(probe, tracker, "unknown", unknown, func() error { return nil }, func() error { return nil })
			requestFailed(probe, tracker, "unknown")
			return nil
		},
		closeContext: func() error { order = append(order, "close"); return nil },
		snapshotUnchanged: func() bool {
			order = append(order, "snapshot")
			got := probe.Evidence()
			if got.State != fabricprobe.AuditAfterOK || !got.AuditAfter || got.SnapshotUnchanged || got.Unchanged {
				t.Fatalf("diagnostic evidence before snapshot comparison: %#v", got)
			}
			return true
		},
	})
	if err == nil || probe.State() != fabricprobe.Rejected || probe.Reason() != fabricprobe.RejectUnknownMutation {
		t.Fatalf("err=%v state=%s reason=%s", err, probe.State(), probe.Reason())
	}
	if want := []string{"route", "online", "audit", "navigate", "wait", "close", "audit", "snapshot"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	evidence := probe.Evidence()
	if !evidence.Aborted || !evidence.RequestFailed || !evidence.ContextClosed || !evidence.AuditAfter || !evidence.SnapshotUnchanged || !evidence.Unchanged {
		t.Fatalf("incomplete diagnostic evidence: %#v", evidence)
	}

	w.Close()
	stderrBytes, _ := io.ReadAll(r)
	stderrStr := string(stderrBytes)
	for _, secret := range []string{"team-id", "channel", "tenant", "user"} {
		if strings.Contains(stderrStr, secret) {
			t.Fatalf("stderr leaked %q: %s", secret, stderrStr)
		}
	}
	allowed := "probe: Fabric mutation intercepted and aborted\n"
	if stderrStr != allowed && stderrStr != "" {
		t.Fatalf("stderr contained unexpected output: %q", stderrStr)
	}
}

func TestLifecycleRejectsUnknownMutationAfterExactMutation(t *testing.T) {
	for _, tc := range []struct {
		name                string
		abortError          bool
		response            bool
		responseBeforeAbort bool
		wantReason          fabricprobe.RejectReason
		wantCount           int
		wantAbort           bool
		wantFailed          bool
	}{
		{"both aborted and failed", false, false, false, fabricprobe.RejectAmbiguousEvent, 2, true, true},
		{"second abort fails", true, false, false, fabricprobe.RejectAbortFailed, 1, false, false},
		{"second response observed", false, true, false, fabricprobe.RejectResponseObserved, 2, true, false},
		{"response before abort returns", false, true, true, fabricprobe.RejectResponseObserved, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, _ := fabricprobe.New(probeTarget)
			tracker := newRequestTracker()
			aborts, audits, closes, snapshots := 0, 0, 0, 0
			err := runLifecycle(context.Background(), probe, lifecycleEdges{
				installRoute: func() error { return nil },
				goOnline:     func() error { return nil },
				audit: func(context.Context) (fabricprobe.Audit, error) {
					audits++
					return fabricprobe.Audit{Exists: true, Role: "User"}, nil
				},
				navigate: func() error { return nil },
				waitForTarget: func(context.Context) error {
					exact := configuredRequest()
					dispatchRoute(probe, tracker, "exact", exact, func() error { aborts++; return nil }, func() error {
						t.Fatal("exact mutation must not continue")
						return nil
					})
					requestFailed(probe, tracker, "exact")

					unknown := configuredRequest()
					unknown.Method = "PATCH"
					dispatchRoute(probe, tracker, "unknown", unknown, func() error {
						aborts++
						if tc.responseBeforeAbort {
							responseObserved(probe, tracker, "unknown")
						}
						if tc.abortError {
							return errors.New("abort failed")
						}
						return nil
					}, func() error {
						t.Fatal("unknown mutation must not continue")
						return nil
					})
					if tc.response && !tc.responseBeforeAbort {
						responseObserved(probe, tracker, "unknown")
					} else if !tc.abortError {
						requestFailed(probe, tracker, "unknown")
					}
					return nil
				},
				closeContext: func() error { closes++; return nil },
				snapshotUnchanged: func() bool {
					snapshots++
					return true
				},
			})
			encoded, marshalErr := json.Marshal(probe.Evidence())
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var evidence fabricprobe.Evidence
			if unmarshalErr := json.Unmarshal(encoded, &evidence); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			if err == nil || aborts != 2 || audits != 1 || closes != 1 || snapshots != 0 || evidence.State != fabricprobe.Rejected || evidence.ErrorCode != tc.wantReason || evidence.Attempts != tc.wantCount || evidence.Aborted != tc.wantAbort || evidence.RequestFailed != tc.wantFailed || evidence.ResponseObserved != tc.response || !evidence.ContextClosed || evidence.AuditAfter || evidence.SnapshotUnchanged || evidence.Unchanged || evidence.Mutation == nil || evidence.Mutation.Method != "PATCH" {
				t.Fatalf("err=%v aborts=%d audits=%d closes=%d snapshots=%d evidence=%s", err, aborts, audits, closes, snapshots, encoded)
			}
			for _, secret := range []string{"secret-candidate", "team-id", "channel@tenant", "user@tenant"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatalf("evidence leaked %q", secret)
				}
			}
		})
	}
}

func TestLifecycleTracksExactAbortStillInFlightWhenUnknownArrives(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	tracker := newRequestTracker()
	aborts := 0
	err, audits, closes, snapshots := runTestLifecycle(probe, func() error {
		dispatchRoute(probe, tracker, "exact", configuredRequest(), func() error {
			aborts++
			unknown := configuredRequest()
			unknown.Method = "PATCH"
			dispatchRoute(probe, tracker, "unknown", unknown, func() error {
				aborts++
				requestFailed(probe, tracker, "unknown") // Event before Abort returns.
				return nil
			}, func() error { t.Fatal("unknown mutation continued"); return nil })
			return nil
		}, func() error { t.Fatal("exact mutation continued"); return nil })
		requestFailed(probe, tracker, "exact")
		return nil
	})
	evidence := probe.Evidence()
	if err == nil || probe.Reason() != fabricprobe.RejectAmbiguousEvent || aborts != 2 || audits != 1 || closes != 1 || snapshots != 0 || evidence.Attempts != 2 || !evidence.Aborted || !evidence.RequestFailed || !evidence.ContextClosed || evidence.AuditAfter {
		t.Fatalf("err=%v aborts=%d audits=%d closes=%d snapshots=%d evidence=%#v", err, aborts, audits, closes, snapshots, evidence)
	}
}

func TestLifecycleCorrelatesRequestFailedBeforeAbortReturns(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	tracker := newRequestTracker()
	err, audits, closes, snapshots := runTestLifecycle(probe, func() error {
		request := configuredRequest()
		request.Method = "PATCH"
		dispatchRoute(probe, tracker, "request", request, func() error {
			requestFailed(probe, tracker, "request")
			return nil
		}, func() error { return errors.New("mutation must not continue") })
		return nil
	})
	evidence := probe.Evidence()
	if err == nil || probe.Reason() != fabricprobe.RejectUnknownMutation || audits != 2 || closes != 1 || snapshots != 1 {
		t.Fatalf("err=%v reason=%s audits=%d closes=%d snapshots=%d", err, probe.Reason(), audits, closes, snapshots)
	}
	if !evidence.Aborted || !evidence.RequestFailed || !evidence.ContextClosed || !evidence.Unchanged {
		t.Fatalf("incomplete inverse-order evidence: %#v", evidence)
	}
}

func TestLifecyclePreservesSecondMutationReasonAfterClose(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	tracker := newRequestTracker()
	err, audits, closes, snapshots := runTestLifecycle(probe, func() error {
		first := configuredRequest()
		first.Method = "PATCH"
		dispatchRoute(probe, tracker, "first", first, func() error { return nil }, func() error { return nil })
		requestFailed(probe, tracker, "first")
		if !probe.DiagnosticReady() {
			t.Fatal("first mutation did not reach diagnostic-ready")
		}
		second := configuredRequest()
		second.Method = "PATCH"
		dispatchRoute(probe, tracker, "second", second, func() error { return nil }, func() error { return nil })
		return nil
	})
	evidence := probe.Evidence()
	if err == nil || probe.Reason() != fabricprobe.RejectAmbiguousEvent || audits != 1 || closes != 1 || snapshots != 0 {
		t.Fatalf("err=%v reason=%s audits=%d closes=%d snapshots=%d", err, probe.Reason(), audits, closes, snapshots)
	}
	if !evidence.ContextClosed || evidence.AuditAfter || evidence.SnapshotUnchanged {
		t.Fatalf("second-mutation evidence = %#v", evidence)
	}
}

func TestLifecyclePreservesLateResponseReasonAfterClose(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	tracker := newRequestTracker()
	err, audits, closes, snapshots := runTestLifecycle(probe, func() error {
		request := configuredRequest()
		request.Method = "PATCH"
		dispatchRoute(probe, tracker, "request", request, func() error { return nil }, func() error { return nil })
		requestFailed(probe, tracker, "request")
		if !probe.DiagnosticReady() {
			t.Fatal("mutation did not reach diagnostic-ready")
		}
		responseObserved(probe, tracker, "request")
		return nil
	})
	evidence := probe.Evidence()
	if err == nil || probe.Reason() != fabricprobe.RejectResponseObserved || audits != 1 || closes != 1 || snapshots != 0 {
		t.Fatalf("err=%v reason=%s audits=%d closes=%d snapshots=%d", err, probe.Reason(), audits, closes, snapshots)
	}
	if !evidence.ResponseObserved || !evidence.ContextClosed || evidence.AuditAfter || evidence.SnapshotUnchanged {
		t.Fatalf("late-response evidence = %#v", evidence)
	}
}

func TestEndpointFingerprintPredicatesEndToEnd(t *testing.T) {
	const base = "https://teams.microsoft.com/fabric/amer/templates/api/teams/team-id/channels/OID:channel@tenant/users?forceSync=false"
	tests := []struct {
		name                                         string
		url                                          string
		scheme, host, path, port, userinfo, fragment bool
		hostClass                                    string
	}{
		{"exact host", base, true, true, true, true, true, true, "teams.microsoft.com"},
		{"http scheme", strings.Replace(base, "https://", "http://", 1), false, true, true, true, true, true, "teams.microsoft.com"},
		{"cloud host", strings.Replace(base, "teams.microsoft.com", "teams.cloud.microsoft", 1), true, false, true, true, true, true, "teams.cloud.microsoft"},
		{"other host", strings.Replace(base, "teams.microsoft.com", "other.example", 1), true, false, true, true, true, true, "other"},
		{"missing host", strings.Replace(base, "https://teams.microsoft.com", "https://", 1), true, false, true, true, true, true, "invalid"},
		{"wrong path", strings.Replace(base, "/users?", "/users/extra?", 1), true, true, false, true, true, true, "teams.microsoft.com"},
		{"explicit port", strings.Replace(base, "teams.microsoft.com", "teams.microsoft.com:443", 1), true, true, true, false, true, true, "teams.microsoft.com"},
		{"userinfo", strings.Replace(base, "https://", "https://probe@", 1), true, true, true, true, false, true, "teams.microsoft.com"},
		{"fragment", base + "#diagnostic", true, true, true, true, true, false, "teams.microsoft.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe, _ := fabricprobe.New(probeTarget)
			tracker := newRequestTracker()
			request := configuredRequest()
			request.Method = "PATCH"
			request.URL = tt.url
			err, audits, closes, snapshots := runTestLifecycle(probe, func() error {
				dispatchRoute(probe, tracker, "request", request, func() error { return nil }, func() error { return nil })
				requestFailed(probe, tracker, "request")
				return nil
			})
			encoded, marshalErr := json.Marshal(probe.Evidence())
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			var evidence fabricprobe.Evidence
			if unmarshalErr := json.Unmarshal(encoded, &evidence); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			if err == nil || audits != 2 || closes != 1 || snapshots != 1 || evidence.State != fabricprobe.Rejected || evidence.ErrorCode != fabricprobe.RejectUnknownMutation || evidence.Mutation == nil {
				t.Fatalf("lifecycle err=%v audits=%d closes=%d snapshots=%d evidence=%s", err, audits, closes, snapshots, encoded)
			}
			got := evidence.Mutation
			if got.SchemeHTTPS != tt.scheme || got.HostClass != tt.hostClass || got.HostMatch != tt.host || got.PathMatch != tt.path || got.PortEmpty != tt.port || got.UserinfoAbsent != tt.userinfo || got.FragmentAbsent != tt.fragment {
				t.Fatalf("endpoint predicates = %#v, want scheme=%v class=%q host=%v path=%v port=%v userinfo=%v fragment=%v", got, tt.scheme, tt.hostClass, tt.host, tt.path, tt.port, tt.userinfo, tt.fragment)
			}
			if !got.QueryMatch || !got.PayloadMatch || !got.CandidatePresent || !evidence.Aborted || !evidence.RequestFailed || evidence.ResponseObserved || !evidence.ContextClosed || !evidence.AuditAfter || !evidence.SnapshotUnchanged || !evidence.Unchanged {
				t.Fatalf("incomplete full evidence: %s", encoded)
			}
		})
	}
}

func TestLifecycleRejectsChangedSnapshot(t *testing.T) {
	probe, _ := fabricprobe.New(probeTarget)
	tracker := newRequestTracker()
	audit := func(context.Context) (fabricprobe.Audit, error) {
		return fabricprobe.Audit{Exists: true, Role: "User"}, nil
	}
	err := runLifecycle(context.Background(), probe, lifecycleEdges{
		installRoute: func() error { return nil },
		goOnline:     func() error { return nil },
		audit:        audit,
		navigate:     func() error { return nil },
		waitForTarget: func(context.Context) error {
			dispatchRoute(probe, tracker, "request", configuredRequest(), func() error { return nil }, func() error { return nil })
			requestFailed(probe, tracker, "request")
			return nil
		},
		closeContext:      func() error { return nil },
		snapshotUnchanged: func() bool { return false },
	})
	if err == nil || probe.Reason() != fabricprobe.RejectSnapshotChanged {
		t.Fatalf("err=%v state=%s reason=%s", err, probe.State(), probe.Reason())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestReadAuditIsBoundedAndReadOnly(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Host != "teams.microsoft.com" || req.Header.Get("Authorization") != "Bearer secret-web-token" {
			t.Fatalf("unexpected audit request: %s %s", req.Method, req.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"members":[{"id":"8:orgid:audit-user","role":"User"}]}`))}, nil
	})}
	audit, err := readAudit(context.Background(), client, "secret-web-token", "19:private@thread.tacv2", "8:orgid:audit-user")
	if err != nil || audit != (fabricprobe.Audit{Exists: true, Role: "User"}) {
		t.Fatalf("audit = %#v, err=%v", audit, err)
	}
}

func TestIsolatedLabPathsRejectsLoosePermissionsAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(root, "profile")
	config := filepath.Join(root, "home", ".config", "lazyteams")
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if !isolatedLabPaths(root, profile, config) {
		t.Fatal("valid isolated paths rejected")
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if isolatedLabPaths(root, profile, config) {
		t.Fatal("world-readable laboratory accepted")
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(root, "escaped-profile")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if isolatedLabPaths(root, link, config) {
		t.Fatal("profile symlink escaping the laboratory accepted")
	}
}

func TestLaunchOptionsBlockServiceWorkers(t *testing.T) {
	options := launchOptions()
	if options.ServiceWorkers != playwright.ServiceWorkerPolicyBlock || options.Headless == nil || *options.Headless || options.Offline == nil || !*options.Offline {
		t.Fatalf("unsafe launch options: %#v", options)
	}
}

func TestPrepareProbeInputsAcquiresLockBeforeCredentialsAndTargetResolution(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	var order []string
	lock, err := helpers.AcquireProfileLock(configDir)
	if err != nil {
		t.Fatal(err)
	}
	_ = lock.Release()

	inputs, err := prepareProbeInputs(
		configDir,
		helpers.AcquireProfileLock,
		func() (*auth.CredentialSet, error) {
			order = append(order, "credentials")
			if _, err := helpers.AcquireProfileLock(configDir); !errors.Is(err, helpers.ErrProfileInUse) {
				t.Fatalf("credentials loaded without profile lock: acquire err=%v", err)
			}
			return &auth.CredentialSet{}, nil
		},
		func(*auth.CredentialSet) (fabricprobe.Target, string, string, error) {
			order = append(order, "target")
			if _, err := helpers.AcquireProfileLock(configDir); !errors.Is(err, helpers.ErrProfileInUse) {
				t.Fatalf("target resolved without profile lock: acquire err=%v", err)
			}
			return probeTarget, "thread", "audit-user", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"credentials", "target"}) || inputs.target != probeTarget || inputs.threadID != "thread" || inputs.auditUser != "audit-user" {
		t.Fatalf("prepared inputs/order = %#v / %v", inputs, order)
	}
	if err := inputs.lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareProbeInputsReleasesLockWhenPreflightFails(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	_, err := prepareProbeInputs(
		configDir,
		helpers.AcquireProfileLock,
		func() (*auth.CredentialSet, error) { return nil, errors.New("load failed") },
		func(*auth.CredentialSet) (fabricprobe.Target, string, string, error) {
			t.Fatal("target resolved after credential failure")
			return fabricprobe.Target{}, "", "", nil
		},
	)
	if err == nil {
		t.Fatal("expected preflight failure")
	}
	lock, err := helpers.AcquireProfileLock(configDir)
	if err != nil {
		t.Fatalf("preflight failure leaked lock: %v", err)
	}
	_ = lock.Release()
}

func TestRunRequiresExplicitRealProbeFlag(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d", code)
	}
}
