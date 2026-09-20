package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agmonetti/lazyteams/internal/fabricprobe"
	"github.com/mxschmitt/playwright-go"
)

var probeTarget = fabricprobe.Target{Team: "team-id", Channel: "OID:channel@tenant", User: "OID:user@tenant", Role: 2}

func configuredRequest() fabricprobe.Request {
	return fabricprobe.Request{
		Method:    "POST",
		URL:       "https://teams.microsoft.com/fabric/amer/templates/api/teams/team-id/channels/OID:channel@tenant/users?forceSync=false",
		FrameURL:  "https://teams.microsoft.com/v2/",
		Body:      []byte(`{"users":[{"id":"OID:user@tenant","role":2}]}`),
		Candidate: "secret-candidate",
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
		closeContext:      func() error { order = append(order, "close"); return nil },
		snapshotUnchanged: func() bool { order = append(order, "snapshot"); return true },
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

func TestRunRequiresExplicitRealProbeFlag(t *testing.T) {
	if code := run(nil); code != 2 {
		t.Fatalf("run(nil) = %d", code)
	}
}
