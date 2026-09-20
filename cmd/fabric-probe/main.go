// fabric-probe is an isolated Stage 7B laboratory binary. It never persists
// the quarantined candidate and emits only sanitized evidence.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/fabricprobe"
	"github.com/agmonetti/lazyteams/internal/helpers"
	"github.com/mxschmitt/playwright-go"
)

const teamsURL = "https://teams.microsoft.com/v2/"

type lifecycleEdges struct {
	installRoute      func() error
	goOnline          func() error
	audit             func(context.Context) (fabricprobe.Audit, error)
	navigate          func() error
	waitForTarget     func(context.Context) error
	closeContext      func() error
	snapshotUnchanged func() bool
}

func runLifecycle(ctx context.Context, probe *fabricprobe.Probe, edges lifecycleEdges) error {
	fail := func() error {
		if probe.State() != fabricprobe.Rejected {
			probe.RejectExternal(fabricprobe.RejectRuntime)
		}
		return errors.New("probe rejected")
	}
	if edges.installRoute() != nil || !probe.InstallRoute() || edges.goOnline() != nil {
		return fail()
	}
	before, err := edges.audit(ctx)
	if err != nil || !probe.AuditBefore(before) {
		return fail()
	}
	if edges.navigate() != nil || edges.waitForTarget(ctx) != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			probe.Timeout()
		} else if errors.Is(ctx.Err(), context.Canceled) {
			probe.Cancel()
		} else {
			probe.RejectExternal(fabricprobe.RejectRuntime)
		}
		return errors.New("probe rejected")
	}
	if edges.closeContext() != nil || !probe.CloseContext() {
		return fail()
	}
	after, err := edges.audit(ctx)
	if err != nil || !probe.AuditAfter(after) {
		return fail()
	}
	if !edges.snapshotUnchanged() {
		probe.RejectExternal(fabricprobe.RejectSnapshotChanged)
		return errors.New("probe rejected")
	}
	if !probe.Finalize() {
		return fail()
	}
	return nil
}

type requestTracker struct {
	mu   sync.Mutex
	next uint64
	ids  map[any]string
}

func newRequestTracker() *requestTracker { return &requestTracker{ids: make(map[any]string)} }

func (t *requestTracker) reserve() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next++
	return strconv.FormatUint(t.next, 10)
}

func (t *requestTracker) track(req any, id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ids[req] = id
}

func (t *requestTracker) lookup(req any) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	id, ok := t.ids[req]
	return id, ok
}

func frameURL(req playwright.Request) (raw string) {
	defer func() { _ = recover() }()
	if frame := req.Frame(); frame != nil {
		return frame.URL()
	}
	return ""
}

func bearer(req playwright.Request) string {
	value, err := req.HeaderValue("authorization")
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}

func requestFailed(probe *fabricprobe.Probe, tracker *requestTracker, key any) {
	if id, ok := tracker.lookup(key); ok {
		probe.RequestFailed(id)
	}
}

func responseObserved(probe *fabricprobe.Probe, tracker *requestTracker, key any) {
	if id, ok := tracker.lookup(key); ok {
		probe.Response(id)
	}
}

func dispatchRoute(probe *fabricprobe.Probe, tracker *requestTracker, key any, candidate fabricprobe.Request, abort, continueRequest func() error) {
	candidate.ID = tracker.reserve()
	switch probe.Classify(candidate) {
	case fabricprobe.Continue:
		if continueRequest() != nil {
			probe.RejectExternal(fabricprobe.RejectRuntime)
		}
	case fabricprobe.AbortTarget:
		tracker.track(key, candidate.ID)
		probe.Aborted(candidate.ID, abort())
	default:
		_ = abort()
	}
}

func installPlaywrightRoute(ctx playwright.BrowserContext, probe *fabricprobe.Probe, tracker *requestTracker) error {
	ctx.OnRequestFailed(func(req playwright.Request) { requestFailed(probe, tracker, req) })
	ctx.OnResponse(func(response playwright.Response) { responseObserved(probe, tracker, response.Request()) })
	return ctx.Route("**/*", func(route playwright.Route) {
		req := route.Request()
		body, _ := req.PostDataBuffer()
		candidate := fabricprobe.Request{Method: req.Method(), URL: req.URL(), FrameURL: frameURL(req), Body: body, Candidate: bearer(req)}
		dispatchRoute(probe, tracker, req, candidate, func() error { return route.Abort("aborted") }, func() error { return route.Continue() })
	})
}

func launchOptions() playwright.BrowserTypeLaunchPersistentContextOptions {
	return playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless:       new(false),
		Offline:        new(true),
		ServiceWorkers: playwright.ServiceWorkerPolicyBlock,
		Args:           []string{"--no-first-run"},
	}
}

func readAudit(ctx context.Context, client *http.Client, token, threadID, userID string) (fabricprobe.Audit, error) {
	rawURL, err := fabricprobe.AuditURL(threadID)
	if err != nil {
		return fabricprobe.Audit{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fabricprobe.Audit{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fabricprobe.Audit{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fabricprobe.Audit{}, errors.New("audit failed")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fabricprobe.Audit{}, err
	}
	return fabricprobe.ParseMemberAudit(body, userID)
}

func within(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isolatedLabPaths(root, profile, config string) bool {
	root, rootErr := filepath.EvalSymlinks(root)
	profile, profileErr := filepath.EvalSymlinks(profile)
	config, configErr := filepath.EvalSymlinks(config)
	info, statErr := os.Stat(root)
	return rootErr == nil && profileErr == nil && configErr == nil && statErr == nil && info.IsDir() && info.Mode().Perm()&0077 == 0 && within(root, profile) && within(root, config)
}

func sanitizedEvidence(probe *fabricprobe.Probe) {
	_ = json.NewEncoder(os.Stdout).Encode(probe.Evidence())
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) != 1 || args[0] != "--execute-real-probe" {
		fmt.Fprintln(os.Stderr, "disabled: explicit --execute-real-probe required")
		return 2
	}
	labRoot, _ := filepath.Abs(os.Getenv("LAZYTEAMS_PROBE_LAB_ROOT"))
	sessionDir, _ := filepath.Abs(os.Getenv("LAZYTEAMS_PROBE_PROFILE"))
	configDir, _ := filepath.Abs(helpers.ConfigDir())
	if !isolatedLabPaths(labRoot, sessionDir, configDir) {
		fmt.Fprintln(os.Stderr, "rejected: invalid isolated laboratory")
		return 1
	}
	role, err := strconv.Atoi(os.Getenv("LAZYTEAMS_FABRIC_PROBE_ROLE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "rejected: invalid target")
		return 1
	}
	target := fabricprobe.Target{Team: os.Getenv("LAZYTEAMS_FABRIC_PROBE_TEAM"), Channel: os.Getenv("LAZYTEAMS_FABRIC_PROBE_CHANNEL"), User: os.Getenv("LAZYTEAMS_FABRIC_PROBE_USER"), Role: role}
	probe, err := fabricprobe.New(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rejected: invalid target")
		return 1
	}
	threadID := os.Getenv("LAZYTEAMS_FABRIC_PROBE_THREAD")
	auditUser := os.Getenv("LAZYTEAMS_FABRIC_PROBE_AUDIT_USER")
	if _, err := fabricprobe.AuditURL(threadID); err != nil || auditUser == "" {
		probe.RejectExternal(fabricprobe.RejectRuntime)
		sanitizedEvidence(probe)
		return 1
	}
	lock, err := helpers.AcquireProfileLock(configDir)
	if err != nil {
		probe.RejectExternal(fabricprobe.RejectRuntime)
		sanitizedEvidence(probe)
		return 1
	}
	defer lock.Release()
	snapshotPath := filepath.Join(configDir, "tokens.env")
	beforeSnapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		probe.RejectExternal(fabricprobe.RejectRuntime)
		sanitizedEvidence(probe)
		return 1
	}
	credentials, err := auth.LoadCredentialSet()
	if err != nil || credentials.Web.Token == "" {
		probe.RejectExternal(fabricprobe.RejectRuntime)
		sanitizedEvidence(probe)
		return 1
	}

	pw, err := playwright.Run()
	if err != nil {
		probe.RejectExternal(fabricprobe.RejectRuntime)
		sanitizedEvidence(probe)
		return 1
	}
	defer pw.Stop()
	browserCtx, err := pw.Firefox.LaunchPersistentContext(sessionDir, launchOptions())
	if err != nil {
		probe.RejectExternal(fabricprobe.RejectRuntime)
		sanitizedEvidence(probe)
		return 1
	}
	closed := false
	defer func() {
		if !closed {
			_ = browserCtx.Close()
		}
	}()

	tracker := newRequestTracker()
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSignals()
	ctx, cancel := context.WithTimeout(signalCtx, 3*time.Minute)
	defer cancel()
	var page playwright.Page
	edges := lifecycleEdges{
		installRoute: func() error { return installPlaywrightRoute(browserCtx, probe, tracker) },
		goOnline:     func() error { return browserCtx.SetOffline(false) },
		audit: func(ctx context.Context) (fabricprobe.Audit, error) {
			return readAudit(ctx, client, credentials.Web.Token, threadID, auditUser)
		},
		navigate: func() error {
			var err error
			page, err = browserCtx.NewPage()
			if err != nil {
				return err
			}
			_, err = page.Goto(teamsURL, playwright.PageGotoOptions{Timeout: playwright.Float(20000)})
			if err == nil {
				fmt.Fprintln(os.Stderr, "probe armed: perform the configured private-channel role change; it will be aborted")
			}
			return err
		},
		waitForTarget: func(ctx context.Context) error {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				switch probe.State() {
				case fabricprobe.RequestFailedConfirmed:
					return nil
				case fabricprobe.Rejected:
					return errors.New("probe rejected")
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
		},
		closeContext: func() error {
			err := browserCtx.Close()
			closed = err == nil
			return err
		},
		snapshotUnchanged: func() bool {
			after, err := os.ReadFile(snapshotPath)
			return err == nil && bytes.Equal(beforeSnapshot, after)
		},
	}
	if runLifecycle(ctx, probe, edges) != nil {
		sanitizedEvidence(probe)
		return 1
	}
	sanitizedEvidence(probe)
	return 0
}
