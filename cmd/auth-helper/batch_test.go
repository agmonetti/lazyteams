package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	playwright "github.com/mxschmitt/playwright-go"
)

func TestBatchPersistenceFailureDoesNotPublishReady(t *testing.T) {
	withBatchEvents(t)
	persistBatchTokenValues = func(string, auth.TokenValues) (*auth.CredentialSet, error) {
		return nil, errors.New("injected persistence failure")
	}
	values := auth.TokenValues{Graph: "new"}
	err := runBatchJobSequence("graph", func() auth.TokenValues { return values }, func(string) error { return nil }, func(string, auth.TokenValues, map[string]string) (bool, error) {
		return persistBatchCheckpoint(t.TempDir(), "graph", values, map[string]string{})
	}, func() string { return "" })
	if err == nil {
		t.Fatal("expected batch failure")
	}
	if finishBatch(err, false) == 0 {
		t.Fatal("persistence failure returned success")
	}
	events := decodeBatchEvents(t)
	assertBatchEvents(t, events, []auth.AuthEvent{
		{Version: auth.EventVersion, Event: "provider_failed", BatchID: "batch", Provider: "graph", Generation: 1, State: "unknown", ErrorCode: "persistence_failed"},
		{Version: auth.EventVersion, Event: "completed", BatchID: "batch", Provider: "batch", Generation: 1, State: "partial_failure", ErrorCode: "acquisition_failed"},
	})
}

func TestBatchSuccessEmitsExactJSONLSequenceAndGeneration(t *testing.T) {
	withBatchEvents(t)
	eventGeneration = 4
	if err := emitEvent("started", "batch", "running", ""); err != nil {
		t.Fatal(err)
	}
	values := auth.TokenValues{}
	err := runBatchJobSequence("graph", func() auth.TokenValues { return values }, func(string) error {
		values.Graph = "graph-new"
		return nil
	}, func(string, auth.TokenValues, map[string]string) (bool, error) {
		eventGeneration = 5
		if err := emitEvent("credential_ready", "graph", "ready", ""); err != nil {
			return false, err
		}
		return true, nil
	}, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := finishBatch(nil, false); got != 0 {
		t.Fatalf("successful batch exit code = %d", got)
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, Event: "started", BatchID: "batch", Provider: "batch", Generation: 4, State: "running"},
		{Version: auth.EventVersion, Event: "credential_ready", BatchID: "batch", Provider: "graph", Generation: 5, State: "ready"},
		{Version: auth.EventVersion, Event: "completed", BatchID: "batch", Provider: "batch", Generation: 5, State: "completed"},
	})
}

func TestBatchCoreReadyThenEduFailureEmitsPartialTerminal(t *testing.T) {
	withBatchEvents(t)
	values := auth.TokenValues{}
	err := runBatchJobSequence("graph,edu", func() auth.TokenValues { return values }, func(job string) error {
		if job == "graph" {
			values.Graph = "graph-new"
			return nil
		}
		return errors.New("EDU unavailable")
	}, func(job string, got auth.TokenValues, last map[string]string) (bool, error) {
		if job == "graph" {
			last["graph"] = got.Graph
			emitEvent("credential_ready", "graph", "ready", "")
			return true, nil
		}
		return false, nil
	}, func() string { return "" })
	if err == nil {
		t.Fatal("expected EDU failure")
	}
	if finishBatch(err, false) == 0 {
		t.Fatal("EDU failure returned success")
	}
	events := decodeBatchEvents(t)
	assertBatchEvents(t, events, []auth.AuthEvent{
		{Version: auth.EventVersion, Event: "credential_ready", BatchID: "batch", Provider: "graph", Generation: 1, State: "ready"},
		{Version: auth.EventVersion, Event: "provider_failed", BatchID: "batch", Provider: "edu", Generation: 1, State: "unknown", ErrorCode: "acquisition_failed"},
		{Version: auth.EventVersion, Event: "completed", BatchID: "batch", Provider: "batch", Generation: 1, State: "partial_failure", ErrorCode: "acquisition_failed"},
	})
}

func withBatchEvents(t *testing.T) {
	t.Helper()
	oldMode, oldWriter, oldID, oldStarted, oldGeneration, oldTerminal, oldWriteErr, oldPersist, oldInteraction := eventMode, eventWriter, eventBatchID, eventStarted, eventGeneration, eventTerminal, eventWriteErr, persistBatchTokenValues, graphInteractionEmitted
	var stream bytes.Buffer
	eventMode, eventWriter, eventBatchID, eventStarted, eventGeneration, eventTerminal, eventWriteErr, graphInteractionEmitted = true, &stream, "batch", time.Now(), 1, false, nil, false
	persistBatchTokenValues = auth.PersistTokenValues
	t.Cleanup(func() {
		eventMode, eventWriter, eventBatchID, eventStarted, eventGeneration, eventTerminal, eventWriteErr, persistBatchTokenValues, graphInteractionEmitted = oldMode, oldWriter, oldID, oldStarted, oldGeneration, oldTerminal, oldWriteErr, oldPersist, oldInteraction
	})
}

func batchEventBytes() []byte {
	switch w := eventWriter.(type) {
	case *bytes.Buffer:
		return w.Bytes()
	case *failWriter:
		return w.buf.Bytes()
	default:
		return nil
	}
}

func decodeBatchEvents(t *testing.T) []auth.AuthEvent {
	t.Helper()
	return decodeBatchEventBytes(t, batchEventBytes())
}

func decodeBatchEventBytes(t *testing.T, data []byte) []auth.AuthEvent {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	events := make([]auth.AuthEvent, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		event, err := auth.DecodeAuthEvent(line)
		if err != nil {
			t.Fatalf("decode event: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func assertBatchEvents(t *testing.T, got, want []auth.AuthEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d: got=%+v", len(got), len(want), got)
	}
	for i := range want {
		actual, expected := got[i], want[i]
		actual.DurationMS = 0
		expected.DurationMS = 0
		if actual != expected {
			t.Fatalf("event[%d] = %+v, want %+v", i, actual, expected)
		}
	}
}

type failWriter struct {
	buf            bytes.Buffer
	writes, failAt int
	failed         bool
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt && !w.failed {
		w.failed = true
		return 0, io.ErrClosedPipe
	}
	return w.buf.Write(p)
}

func TestIntermediateEventWriteFailureMakesBatchFail(t *testing.T) {
	withBatchEvents(t)
	eventWriter = &failWriter{failAt: 2}
	if err := emitEvent("started", "batch", "running", ""); err != nil {
		t.Fatal(err)
	}
	if err := emitEvent("credential_ready", "graph", "ready", ""); err == nil {
		t.Fatal("expected intermediate write failure")
	}
	if got := finishBatch(nil, false); got == 0 {
		t.Fatal("intermediate protocol failure returned success")
	}
	if writer, ok := eventWriter.(*failWriter); !ok || writer.writes != 3 || !writer.failed {
		t.Fatalf("writer = %#v, want one intermediate failure followed by terminal write", eventWriter)
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, Event: "started", BatchID: "batch", Provider: "batch", Generation: 1, State: "running"},
		{Version: auth.EventVersion, Event: "completed", BatchID: "batch", Provider: "batch", Generation: 1, State: "partial_failure", ErrorCode: "protocol_error"},
	})
}

func TestGraphNetworkFailureEmitsNetworkUnavailable(t *testing.T) {
	withBatchEvents(t)
	err := runBatchJobSequence("graph", func() auth.TokenValues { return auth.TokenValues{} }, func(string) error {
		return errGraphNetwork
	}, func(string, auth.TokenValues, map[string]string) (bool, error) {
		t.Fatal("network failure must not reach persistence")
		return false, nil
	}, func() string { return "" })
	if !errors.Is(err, errGraphNetwork) {
		t.Fatalf("batch error = %v, want network error", err)
	}
	if finishBatch(err, false) == 0 {
		t.Fatal("network failure returned success")
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 1, Event: "provider_failed", State: "unknown", ErrorCode: "network_unavailable"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 1, Event: "completed", State: "partial_failure", ErrorCode: "network_unavailable"},
	})
}

func TestBatchGraphErrorsKeepContractCodes(t *testing.T) {
	if got := batchErrorCode(errGraphCancelled); got != "cancelled" {
		t.Fatalf("cancelled code = %q", got)
	}
	if got := batchErrorCode(errGraphTimeout); got != "timeout" {
		t.Fatalf("timeout code = %q", got)
	}
	if got := batchErrorCode(errGraphNetwork); got != "network_unavailable" {
		t.Fatalf("network code = %q", got)
	}
	if got := batchErrorCode(auth.ErrStaleGeneration); got != "stale_generation" {
		t.Fatalf("stale code = %q", got)
	}
	if got := batchErrorCode(auth.ErrIncompatibleCredential); got != "incompatible_credential" {
		t.Fatalf("incompatible code = %q", got)
	}
}

func TestGraphLifecycleTransitionsHeadlessToVisibleBeforePersisting(t *testing.T) {
	var headlessCalls, visibleCalls, persistCalls, interactionCalls int
	states := []string{"interaction_required", "ready"}
	err := runGraphLifecycle(time.Second,
		func() (string, error) {
			headlessCalls++
			return states[headlessCalls-1], nil
		},
		func() (string, error) {
			visibleCalls++
			return "ready", nil
		},
		func() error {
			persistCalls++
			return nil
		},
		func() error {
			interactionCalls++
			return nil
		},
		func() bool { return false },
	)
	if err != nil {
		t.Fatal(err)
	}
	if headlessCalls != 1 || visibleCalls != 1 || interactionCalls != 1 || persistCalls != 1 {
		t.Fatalf("calls = headless:%d visible:%d interaction:%d persist:%d", headlessCalls, visibleCalls, interactionCalls, persistCalls)
	}
}

func TestGraphLifecycleStopsAfterVisibleInteractionRequired(t *testing.T) {
	visibleCalls, persistCalls := 0, 0
	err := runGraphLifecycle(time.Second,
		func() (string, error) { return "interaction_required", nil },
		func() (string, error) {
			visibleCalls++
			return "interaction_required", nil
		},
		func() error { persistCalls++; return nil },
		func() error { return nil },
		func() bool { return false },
	)
	if !errors.Is(err, errGraphInteractionRequired) {
		t.Fatalf("lifecycle error = %v, want interaction required", err)
	}
	if visibleCalls != 1 || persistCalls != 0 {
		t.Fatalf("calls = visible:%d persist:%d, want 1 and 0", visibleCalls, persistCalls)
	}
}

func TestGraphBatchInteractionPublishesReadyAfterVisibleFlow(t *testing.T) {
	withBatchEvents(t)
	values := auth.TokenValues{}
	err := runBatchJobSequence("graph", func() auth.TokenValues { return values }, func(string) error {
		err := runGraphLifecycle(time.Second,
			func() (string, error) { return "interaction_required", nil },
			func() (string, error) { values.Graph = "graph-visible"; return "ready", nil },
			func() error { eventGeneration = 8; return nil },
			func() error { return nil },
			func() bool { return false },
		)
		return err
	}, func(job string, got auth.TokenValues, last map[string]string) (bool, error) {
		if job != "graph" || got.Graph != "graph-visible" {
			return false, errors.New("graph was not captured")
		}
		last["graph"] = got.Graph
		if err := emitEvent("credential_ready", "graph", "ready", ""); err != nil {
			return false, err
		}
		return true, nil
	}, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := finishBatch(nil, false); got != 0 {
		t.Fatalf("successful interaction batch exit code = %d", got)
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 8, Event: "credential_ready", State: "ready"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 8, Event: "completed", State: "completed"},
	})
}

func TestGraphBatchInteractionJSONLIncludesIntermediateState(t *testing.T) {
	withBatchEvents(t)
	eventGeneration = 3
	values := auth.TokenValues{}
	if err := emitEvent("started", "batch", "running", ""); err != nil {
		t.Fatal(err)
	}
	err := runBatchJobSequence("graph", func() auth.TokenValues { return values }, func(string) error {
		if err := emitEvent("interaction_required", "graph", "interaction_required", "interaction_required"); err != nil {
			return err
		}
		values.Graph = "graph-visible"
		eventGeneration = 4
		return nil
	}, func(job string, got auth.TokenValues, last map[string]string) (bool, error) {
		if job != "graph" || got.Graph != "graph-visible" {
			return false, errors.New("graph was not captured")
		}
		last["graph"] = got.Graph
		if err := emitEvent("credential_ready", "graph", "ready", ""); err != nil {
			return false, err
		}
		return true, nil
	}, func() string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got := finishBatch(nil, false); got != 0 {
		t.Fatalf("successful interaction batch exit code = %d", got)
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 3, Event: "started", State: "running"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 3, Event: "interaction_required", State: "interaction_required", ErrorCode: "interaction_required"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 4, Event: "credential_ready", State: "ready"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 4, Event: "completed", State: "completed"},
	})
}

func TestProductionGraphBatchPreservesIncompatibleCredential(t *testing.T) {
	oldLifecycle, oldPersist := runGraphLifecycleRunner, persistGraphCandidate
	t.Cleanup(func() { runGraphLifecycleRunner, persistGraphCandidate = oldLifecycle, oldPersist })
	withBatchEvents(t)
	persistCalls := 0
	persistGraphCandidate = func(string, auth.GraphCandidate) (*auth.CredentialSet, error) {
		persistCalls++
		if persistCalls == 1 {
			return nil, auth.ErrIncompatibleCredential
		}
		return &auth.CredentialSet{Generation: 9}, nil
	}
	captured := &tokens{graphCandidate: &auth.GraphCandidate{Token: "invalid"}}
	runGraphLifecycleRunner = func(_ time.Duration, _ func() (string, error), _ func() (string, error), persist func() error, _ func() error, _ func() bool) error {
		if err := persist(); err != nil {
			if !errors.Is(err, auth.ErrIncompatibleCredential) {
				return err
			}
			captured.mu.Lock()
			captured.graphCandidate = &auth.GraphCandidate{Token: "valid"}
			captured.mu.Unlock()
		}
		return persist()
	}
	_, _, err := runBatchJobsState(nil, t.TempDir(), "graph", nil, nil, captured, t.TempDir())
	if err != nil {
		t.Fatalf("batch error = %v, want valid candidate after incompatible one", err)
	}
	captured.mu.Lock()
	defer captured.mu.Unlock()
	if captured.graphToken != "valid" || !captured.graphPersisted || persistCalls != 2 {
		t.Fatalf("capture = token:%q persisted:%v calls:%d", captured.graphToken, captured.graphPersisted, persistCalls)
	}
}

func TestProductionBatchEmitsIncompatibleTerminalProtocol(t *testing.T) {
	withBatchEvents(t)
	if err := emitEvent("started", "batch", "running", ""); err != nil {
		t.Fatal(err)
	}
	err := runBatchJobSequence("graph", func() auth.TokenValues {
		return auth.TokenValues{}
	}, func(string) error {
		return auth.ErrIncompatibleCredential
	}, func(string, auth.TokenValues, map[string]string) (bool, error) {
		t.Fatal("incompatible candidate must not reach checkpoint")
		return false, nil
	}, func() string { return "" })
	if !errors.Is(err, auth.ErrIncompatibleCredential) {
		t.Fatalf("batch error = %v, want incompatible credential", err)
	}
	if got := finishBatch(err, false); got == 0 {
		t.Fatal("incompatible credential returned success")
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 1, Event: "started", State: "running"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 1, Event: "provider_failed", State: "unknown", ErrorCode: "incompatible_credential"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 1, Event: "completed", State: "partial_failure", ErrorCode: "incompatible_credential"},
	})
}

func TestGraphLifecycleRetriesInvalidCandidateBeforeValid(t *testing.T) {
	persistCalls := 0
	err := runGraphLifecycle(time.Second,
		func() (string, error) { return "ready", nil },
		func() (string, error) { t.Fatal("visible fallback must not open"); return "", nil },
		func() error {
			persistCalls++
			if persistCalls == 1 {
				return auth.ErrIncompatibleCredential
			}
			return nil
		},
		func() error { t.Fatal("interaction must not be requested"); return nil },
		func() bool { return false },
	)
	if err != nil {
		t.Fatal(err)
	}
	if persistCalls != 2 {
		t.Fatalf("persist calls = %d, want 2", persistCalls)
	}
}

func TestGraphWaitStopsWhenContextRegistryIsClosing(t *testing.T) {
	activeContexts = contextRegistry{}
	activeContexts.mu.Lock()
	activeContexts.closing = true
	activeContexts.mu.Unlock()
	t.Cleanup(func() { activeContexts = contextRegistry{} })
	if got := waitForVisibleGraphCandidate(nil, &tokens{}, time.Second); got != "cancelled" {
		t.Fatalf("wait result = %q, want cancelled", got)
	}
}

func TestGraphLifecycleCancelsBeforeVisibleOrPersistence(t *testing.T) {
	closed := false
	headlessCalls := 0
	persistCalls := 0
	err := runGraphLifecycle(time.Second,
		func() (string, error) {
			headlessCalls++
			return "interaction_required", nil
		},
		func() (string, error) { t.Fatal("cancelled lifecycle must not open visible"); return "", nil },
		func() error { persistCalls++; return nil },
		func() error { closed = true; return nil },
		func() bool { return closed },
	)
	if !errors.Is(err, errGraphCancelled) {
		t.Fatalf("lifecycle error = %v, want cancellation", err)
	}
	if headlessCalls != 1 || persistCalls != 0 {
		t.Fatalf("calls = headless:%d persist:%d, want 1 and 0", headlessCalls, persistCalls)
	}
}

type fakeBrowserContext struct {
	playwright.BrowserContext
	id string
}

func (f *fakeBrowserContext) OnClose(func(playwright.BrowserContext))        {}
func (f *fakeBrowserContext) Cookies(...string) ([]playwright.Cookie, error) { return nil, nil }

type requestEventContext struct {
	playwright.BrowserContext
	requestHandler func(playwright.Request)
}

func (c *requestEventContext) On(name string, handler any) {
	if name == "request" {
		c.requestHandler = handler.(func(playwright.Request))
	}
}

type fakeRequest struct {
	playwright.Request
	url     string
	headers map[string]string
}

func (r *fakeRequest) URL() string                { return r.url }
func (r *fakeRequest) Headers() map[string]string { return r.headers }

func TestFullInterceptorIgnoresFabricReadRequest(t *testing.T) {
	ctx := &requestEventContext{}
	captured := &tokens{}
	registerFullInterceptor(ctx, captured)
	if ctx.requestHandler == nil {
		t.Fatal("full interceptor did not register request handler")
	}
	ctx.requestHandler(&fakeRequest{
		url:     "https://teams.microsoft.com/api/fabric/read/members",
		headers: map[string]string{"authorization": "Bearer opaque-read-token"},
	})
	if captured.fabricToken != "" {
		t.Fatal("Fabric read request produced a candidate")
	}
}

func TestRunPublicBatchTraversesGraphLifecycleAndCleansReplacement(t *testing.T) {
	oldArgs := os.Args
	oldStdout := os.Stdout
	oldSetup, oldInstall, oldPlaywrightRun := runSetupHook, runPlaywrightInstall, runPlaywrightRun
	oldOpen, oldPages, oldNewPage := runOpenPersistentContext, runContextPages, runContextNewPage
	oldViewport, oldOnClose, oldRegister, oldClose := runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext
	oldCapture, oldOutcome, oldVisibleOutcome := runBatchGraphCapture, runBatchGraphOutcome, runBatchVisibleGraphOutcome
	oldBatchClose, oldBatchNewPage, oldBatchRegister := runBatchContextClose, runBatchContextNewPage, runBatchRegisterInterceptor
	oldPersist, oldEventMode, oldWriter := persistGraphCandidate, eventMode, eventWriter
	oldTerminal, oldWriteErr, oldGeneration := eventTerminal, eventWriteErr, eventGeneration
	t.Cleanup(func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
		runSetupHook, runPlaywrightInstall, runPlaywrightRun = oldSetup, oldInstall, oldPlaywrightRun
		runOpenPersistentContext, runContextPages, runContextNewPage = oldOpen, oldPages, oldNewPage
		runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext = oldViewport, oldOnClose, oldRegister, oldClose
		runBatchGraphCapture, runBatchGraphOutcome, runBatchVisibleGraphOutcome = oldCapture, oldOutcome, oldVisibleOutcome
		runBatchContextClose, runBatchContextNewPage, runBatchRegisterInterceptor = oldBatchClose, oldBatchNewPage, oldBatchRegister
		persistGraphCandidate, eventMode, eventWriter = oldPersist, oldEventMode, oldWriter
		eventTerminal, eventWriteErr, eventGeneration = oldTerminal, oldWriteErr, oldGeneration
	})
	t.Setenv("HOME", t.TempDir())
	var out bytes.Buffer
	var events []string
	setupCalled := false
	active := 0
	maxActive := 0
	openCalls, closeCalls := 0, 0
	initialContext := &fakeBrowserContext{id: "headless"}
	visibleContext := &fakeBrowserContext{id: "visible"}
	closedIDs := []string{}
	runSetupHook = func() { setupCalled = true }
	runPlaywrightInstall = func(...*playwright.RunOptions) error { return nil }
	runPlaywrightRun = func(...*playwright.RunOptions) (*playwright.Playwright, error) { return nil, nil }
	runOpenPersistentContext = func(*playwright.Playwright, string, bool) (playwright.BrowserContext, error) {
		openCalls++
		active++
		if active > maxActive {
			maxActive = active
		}
		if openCalls == 1 {
			return initialContext, nil
		}
		return visibleContext, nil
	}
	runContextPages = func(playwright.BrowserContext) []playwright.Page { return nil }
	runContextNewPage = func(playwright.BrowserContext) (playwright.Page, error) { return nil, nil }
	runPageSetViewport = func(playwright.Page, int, int) error { return nil }
	runContextOnClose = func(playwright.BrowserContext, func()) {}
	runRegisterInterceptor = func(playwright.BrowserContext, *tokens) {}
	runCloseContext = func(ctx playwright.BrowserContext) error {
		closeCalls++
		if got, ok := ctx.(*fakeBrowserContext); ok {
			closedIDs = append(closedIDs, got.id)
		}
		if active > 0 {
			active--
		}
		return nil
	}
	runBatchContextClose = func(ctx playwright.BrowserContext) error {
		closeCalls++
		if got, ok := ctx.(*fakeBrowserContext); ok {
			closedIDs = append(closedIDs, got.id)
		}
		if active > 0 {
			active--
		}
		return nil
	}
	runBatchContextNewPage = func(playwright.BrowserContext) (playwright.Page, error) { return nil, nil }
	runBatchRegisterInterceptor = func(playwright.BrowserContext, *tokens) {}
	runBatchGraphCapture = func(_ playwright.Page, captured *tokens, _ bool) error {
		captured.mu.Lock()
		captured.graphCandidate = &auth.GraphCandidate{Token: "candidate"}
		captured.mu.Unlock()
		return nil
	}
	runBatchGraphOutcome = func(_ playwright.Page, _ *tokens, _ time.Duration) string { return "interaction_required" }
	runBatchVisibleGraphOutcome = func(_ playwright.Page, _ *tokens, _ time.Duration) string { return "ready" }
	persistGraphCandidate = func(string, auth.GraphCandidate) (*auth.CredentialSet, error) {
		return &auth.CredentialSet{Generation: 7}, nil
	}
	eventMode, eventWriter = true, &out
	eventTerminal, eventWriteErr, eventGeneration = false, nil, 0
	eventStarted = time.Now()
	os.Args = []string{"lazyteams-auth", "--events", "--renew-batch", "graph"}
	if got := run(); got != 0 {
		t.Fatalf("run exit = %d, want 0", got)
	}
	if !setupCalled || openCalls != 2 || closeCalls != 2 || maxActive != 1 {
		t.Fatalf("setup/open/close/max = %v/%d/%d/%d, want true/2/2/1", setupCalled, openCalls, closeCalls, maxActive)
	}
	if got, want := strings.Join(closedIDs, ","), "headless,visible"; got != want {
		t.Fatalf("closed contexts = %q, want %q", got, want)
	}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			events = append(events, line)
		}
	}
	joined := strings.Join(events, "\n")
	if !strings.Contains(joined, `"event":"interaction_required"`) || !strings.Contains(joined, `"event":"credential_ready"`) || !strings.Contains(joined, `"event":"completed"`) {
		t.Fatalf("lifecycle events = %s", joined)
	}
}

func TestRunBatchJobsStateEmitsIncompatibleTerminalProtocol(t *testing.T) {
	withBatchEvents(t)
	oldCapture, oldOutcome, oldPersist := runBatchGraphCapture, runBatchGraphOutcome, persistGraphCandidate
	t.Cleanup(func() {
		runBatchGraphCapture, runBatchGraphOutcome, persistGraphCandidate = oldCapture, oldOutcome, oldPersist
	})
	captures := 0
	runBatchGraphCapture = func(_ playwright.Page, captured *tokens, _ bool) error {
		captures++
		if captures == 1 {
			captured.graphCandidate = &auth.GraphCandidate{Token: "candidate"}
		}
		return nil
	}
	runBatchGraphOutcome = func(_ playwright.Page, _ *tokens, _ time.Duration) string {
		if captures == 1 {
			return "ready"
		}
		return "timeout"
	}
	persistGraphCandidate = func(string, auth.GraphCandidate) (*auth.CredentialSet, error) {
		return nil, auth.ErrIncompatibleCredential
	}
	_, _, err := runBatchJobsState(nil, t.TempDir(), "graph", nil, nil, &tokens{}, t.TempDir())
	if !errors.Is(err, auth.ErrIncompatibleCredential) {
		t.Fatalf("batch error = %v, want incompatible credential", err)
	}
	if code := finishBatch(err, false); code == 0 {
		t.Fatal("incompatible batch returned success")
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 1, Event: "provider_failed", State: "unknown", ErrorCode: "incompatible_credential"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 1, Event: "completed", State: "partial_failure", ErrorCode: "incompatible_credential"},
	})
}

func TestRunPublicFabricRenewalFailsWithoutSaving(t *testing.T) {
	oldArgs, oldStdout := os.Args, os.Stdout
	oldSetup, oldInstall, oldRun, oldPatch := runSetupHook, runPlaywrightInstall, runPlaywrightRun, runPatchWebSocketAsserts
	oldOpen, oldPages, oldNewPage := runOpenPersistentContext, runContextPages, runContextNewPage
	oldViewport, oldOnClose, oldRegister, oldClose := runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext
	oldManual, oldSave := runManualFabricCapture, runSaveTokens
	t.Cleanup(func() {
		os.Args, os.Stdout = oldArgs, oldStdout
		runSetupHook, runPlaywrightInstall, runPlaywrightRun, runPatchWebSocketAsserts = oldSetup, oldInstall, oldRun, oldPatch
		runOpenPersistentContext, runContextPages, runContextNewPage = oldOpen, oldPages, oldNewPage
		runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext = oldViewport, oldOnClose, oldRegister, oldClose
		runManualFabricCapture, runSaveTokens = oldManual, oldSave
		activeContexts = contextRegistry{}
	})
	t.Setenv("HOME", t.TempDir())
	runSetupHook = func() {}
	runPlaywrightInstall = func(...*playwright.RunOptions) error { return nil }
	runPlaywrightRun = func(...*playwright.RunOptions) (*playwright.Playwright, error) { return nil, nil }
	runPatchWebSocketAsserts = func() {}
	runOpenPersistentContext = func(*playwright.Playwright, string, bool) (playwright.BrowserContext, error) { return nil, nil }
	runContextPages = func(playwright.BrowserContext) []playwright.Page { return nil }
	runContextNewPage = func(playwright.BrowserContext) (playwright.Page, error) { return nil, nil }
	runPageSetViewport = func(playwright.Page, int, int) error { return nil }
	runContextOnClose = func(playwright.BrowserContext, func()) {}
	runRegisterInterceptor = func(playwright.BrowserContext, *tokens) {}
	runCloseContext = func(playwright.BrowserContext) error { return nil }
	manualCalls, saveCalls := 0, 0
	runManualFabricCapture = func(*playwright.Playwright, string, *tokens) { manualCalls++ }
	runSaveTokens = func(*tokens, string) error { saveCalls++; return nil }

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writePipe
	os.Args = []string{"lazyteams-auth", "--renew", "fabric"}
	code := run()
	if err := writePipe.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatal(err)
	}
	if code == 0 || manualCalls != 1 || saveCalls != 0 {
		t.Fatalf("exit/manual/save = %d/%d/%d, want nonzero/1/0", code, manualCalls, saveCalls)
	}
	if strings.Contains(string(output), "Session saved") {
		t.Fatalf("Fabric fallback announced persistence: %s", output)
	}
}

func TestFullRenewalNeverCapturesFabric(t *testing.T) {
	oldGoto, oldCapture, oldSleep, oldGraph := fullRenewalGoto, fullRenewalCapture, fullRenewalSleep, fullRenewalGraph
	oldManual := runManualFabricCapture
	t.Cleanup(func() {
		fullRenewalGoto, fullRenewalCapture, fullRenewalSleep, fullRenewalGraph = oldGoto, oldCapture, oldSleep, oldGraph
		runManualFabricCapture = oldManual
	})
	fullRenewalGoto = func(playwright.Page) error { return nil }
	fullRenewalCapture = func(playwright.Page, playwright.BrowserContext, *playwright.Playwright, string, *tokens, <-chan struct{}) string {
		return captureOK
	}
	fullRenewalSleep = func(time.Duration) {}
	fullRenewalGraph = func(_ *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, _ *tokens, _, _ string) (playwright.Page, playwright.BrowserContext, error) {
		return page, ctx, nil
	}
	manualCalls := 0
	runManualFabricCapture = func(*playwright.Playwright, string, *tokens) { manualCalls++ }
	ctx := &fakeBrowserContext{}
	if _, _, err := fullRenewal(nil, nil, ctx, &tokens{}, "session", false, make(chan struct{}), "config"); err != nil {
		t.Fatal(err)
	}
	if manualCalls != 0 {
		t.Fatalf("full renewal invoked Fabric capture %d times", manualCalls)
	}
}

func TestRunPublicEntryPointExecutesSetupWithoutShortCircuit(t *testing.T) {
	oldSetup, oldArgs := runSetupHook, os.Args
	t.Cleanup(func() { runSetupHook, os.Args = oldSetup, oldArgs })
	called := false
	runSetupHook = func() { called = true }
	os.Args = []string{"lazyteams-auth", "--help"}
	if got := run(); got != 0 || !called {
		t.Fatalf("run setup = code:%d called:%v, want 0/true", got, called)
	}
}

func TestRunPublicSetupUsesProductiveCleanupWithoutLaunchingBrowser(t *testing.T) {
	oldSetup, oldArgs, oldLauncher := runSetupHook, os.Args, runOpenPersistentContext
	t.Cleanup(func() { runSetupHook, os.Args, runOpenPersistentContext = oldSetup, oldArgs, oldLauncher })
	t.Setenv("HOME", t.TempDir())
	setupCalled, launchCalls := false, 0
	runSetupHook = func() { setupCalled = true }
	runOpenPersistentContext = func(*playwright.Playwright, string, bool) (playwright.BrowserContext, error) {
		launchCalls++
		return nil, errors.New("browser launch must not be reached")
	}
	os.Args = []string{"lazyteams-auth", "--clear-session", "--clear-tokens"}
	if got := run(); got != 0 {
		t.Fatalf("run setup exit = %d, want 0", got)
	}
	if !setupCalled || launchCalls != 0 {
		t.Fatalf("setup/launcher calls = %v/%d, want true/0", setupCalled, launchCalls)
	}
}

func TestRunPublicEntryPointTraversesInjectedSetupAndDispatch(t *testing.T) {
	oldArgs := os.Args
	oldSetup, oldInstall, oldPlaywrightRun := runSetupHook, runPlaywrightInstall, runPlaywrightRun
	oldBatch := runBatchJobsStateRunner
	oldOpen, oldPages, oldNewPage := runOpenPersistentContext, runContextPages, runContextNewPage
	oldViewport, oldOnClose, oldRegister, oldClose := runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext
	oldGraph := graphRenewalRunner
	t.Cleanup(func() {
		os.Args = oldArgs
		runSetupHook, runPlaywrightInstall, runPlaywrightRun = oldSetup, oldInstall, oldPlaywrightRun
		runBatchJobsStateRunner = oldBatch
		runOpenPersistentContext, runContextPages, runContextNewPage = oldOpen, oldPages, oldNewPage
		runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext = oldViewport, oldOnClose, oldRegister, oldClose
		graphRenewalRunner = oldGraph
	})
	t.Setenv("HOME", t.TempDir())
	setupCalled, launchCalls, dispatchCalled, batchCalls, closeCalls := false, 0, false, 0, 0
	runSetupHook = func() { setupCalled = true }
	runPlaywrightInstall = func(...*playwright.RunOptions) error { return nil }
	runPlaywrightRun = func(...*playwright.RunOptions) (*playwright.Playwright, error) { return nil, nil }
	runOpenPersistentContext = func(*playwright.Playwright, string, bool) (playwright.BrowserContext, error) {
		launchCalls++
		return nil, nil
	}
	runContextPages = func(playwright.BrowserContext) []playwright.Page { return nil }
	runContextNewPage = func(playwright.BrowserContext) (playwright.Page, error) { return nil, nil }
	runPageSetViewport = func(playwright.Page, int, int) error { return nil }
	runContextOnClose = func(playwright.BrowserContext, func()) {}
	runRegisterInterceptor = func(playwright.BrowserContext, *tokens) {}
	runCloseContext = func(playwright.BrowserContext) error { closeCalls++; return nil }
	runBatchJobsStateRunner = func(_ *playwright.Playwright, _ string, _ string, page playwright.Page, ctx playwright.BrowserContext, _ *tokens, _ string) (playwright.Page, playwright.BrowserContext, error) {
		batchCalls++
		return page, ctx, nil
	}
	graphRenewalRunner = func(batch func() error) error {
		dispatchCalled = true
		return batch()
	}
	os.Args = []string{"lazyteams-auth", "--renew", "graph"}
	if got := run(); got != 0 {
		t.Fatalf("run exit = %d, want 0", got)
	}
	if !setupCalled || !dispatchCalled || batchCalls != 1 || launchCalls != 1 || closeCalls != 1 {
		t.Fatalf("setup/dispatch/batch/launch/close = %v/%v/%d/%d/%d, want true/true/1/1/1", setupCalled, dispatchCalled, batchCalls, launchCalls, closeCalls)
	}
}

func TestRunPersistentContextLauncherIsInstrumentable(t *testing.T) {
	old := runOpenPersistentContext
	t.Cleanup(func() { runOpenPersistentContext = old })
	called := false
	runOpenPersistentContext = func(_ *playwright.Playwright, sessionDir string, headless bool) (playwright.BrowserContext, error) {
		called = sessionDir == "session" && headless
		return nil, nil
	}
	if _, err := openRunPersistentContext(nil, "session", true); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("run persistent context launcher seam was not invoked")
	}
}

func TestRenewalDispatchSeamsExecutePublicEntryPoints(t *testing.T) {
	oldGraph, oldFull := graphRenewalRunner, fullRenewalRunner
	t.Cleanup(func() { graphRenewalRunner, fullRenewalRunner = oldGraph, oldFull })
	graphCalled, fullCalled := false, false
	graphRenewalRunner = func(batch func() error) error {
		graphCalled = true
		return batch()
	}
	fullRenewalRunner = func(_ *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, _ *tokens, _ string, _ bool, _ <-chan struct{}, _ string) (playwright.Page, playwright.BrowserContext, error) {
		fullCalled = true
		return page, ctx, nil
	}
	if _, _, err := dispatchRenewal("graph", func() (playwright.Page, playwright.BrowserContext, error) {
		err := graphRenewalRunner(func() error { return nil })
		return nil, nil, err
	}, func() (playwright.Page, playwright.BrowserContext, error) {
		return nil, nil, errors.New("wrong dispatch")
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dispatchRenewal("", func() (playwright.Page, playwright.BrowserContext, error) {
		return nil, nil, errors.New("wrong dispatch")
	}, func() (playwright.Page, playwright.BrowserContext, error) {
		return fullRenewalRunner(nil, nil, nil, &tokens{}, "", false, nil, "")
	}); err != nil {
		t.Fatal(err)
	}
	if !graphCalled || !fullCalled {
		t.Fatalf("dispatch seams called = graph:%v full:%v", graphCalled, fullCalled)
	}
}

func TestFullRenewalExecutesBodyWithInjectedLifecycle(t *testing.T) {
	oldGoto, oldCapture, oldSleep := fullRenewalGoto, fullRenewalCapture, fullRenewalSleep
	oldGraph, oldPost := fullRenewalGraph, fullRenewalPostGraph
	t.Cleanup(func() {
		fullRenewalGoto, fullRenewalCapture, fullRenewalSleep = oldGoto, oldCapture, oldSleep
		fullRenewalGraph, fullRenewalPostGraph = oldGraph, oldPost
	})
	gotoCalled, captureCalled, graphCalled, postCalled := false, false, false, false
	fullRenewalGoto = func(playwright.Page) error { gotoCalled = true; return nil }
	fullRenewalCapture = func(playwright.Page, playwright.BrowserContext, *playwright.Playwright, string, *tokens, <-chan struct{}) string {
		captureCalled = true
		return captureOK
	}
	fullRenewalSleep = func(time.Duration) {}
	fullRenewalGraph = func(_ *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, _ *tokens, _, _ string) (playwright.Page, playwright.BrowserContext, error) {
		graphCalled = true
		return page, ctx, nil
	}
	fullRenewalPostGraph = func(_ *playwright.Playwright, _ playwright.Page, _ playwright.BrowserContext, _ *tokens, _ string) error {
		postCalled = true
		return nil
	}
	page, ctx, err := fullRenewal(nil, nil, nil, &tokens{}, "", false, make(chan struct{}), "")
	if err != nil {
		t.Fatal(err)
	}
	if page != nil || ctx != nil || !gotoCalled || !captureCalled || !graphCalled || !postCalled {
		t.Fatalf("state/hooks = page:%v ctx:%v goto:%v capture:%v graph:%v post:%v", page, ctx, gotoCalled, captureCalled, graphCalled, postCalled)
	}
}

func TestFullRenewalFlowContinuesAfterGraph(t *testing.T) {
	var order []string
	if err := runFullRenewalFlow(func() error {
		order = append(order, "graph")
		return nil
	}, func() error {
		order = append(order, "post-graph")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(order, ","), "graph,post-graph"; got != want {
		t.Fatalf("renewal order = %q, want %q", got, want)
	}
}

func TestFullRenewalFlowStopsBeforePostGraphOnError(t *testing.T) {
	want := errors.New("graph failed")
	postCalled := false
	if err := runFullRenewalFlow(func() error { return want }, func() error {
		postCalled = true
		return nil
	}); !errors.Is(err, want) {
		t.Fatalf("renewal error = %v, want %v", err, want)
	}
	if postCalled {
		t.Fatal("post-Graph flow ran after Graph failure")
	}
}

func TestRunGraphRenewalPropagatesBatchResult(t *testing.T) {
	want := errors.New("batch failed")
	if err := runGraphRenewal(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("graph renewal error = %v, want %v", err, want)
	}
}

func TestRenewGraphIfNeededSkipsAcquisitionForExistingCredential(t *testing.T) {
	called := false
	if err := renewGraphIfNeeded(&tokens{graphToken: "existing"}, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("existing Graph credential triggered acquisition")
	}
}

func TestRenewGraphIfNeededRequiresPersistence(t *testing.T) {
	if err := renewGraphIfNeeded(&tokens{}, func() error { return nil }); !errors.Is(err, errGraphTimeout) {
		t.Fatalf("unpersisted Graph renewal error = %v, want timeout", err)
	}
}

func TestFullRenewalSkipsGraphAcquisitionWhenCredentialExists(t *testing.T) {
	captured := &tokens{graphToken: "existing"}
	if graphCaptureNeeded(captured) {
		t.Fatal("existing Graph credential must skip acquisition")
	}
	if !graphCaptureNeeded(&tokens{}) {
		t.Fatal("missing Graph credential must request acquisition")
	}
}

func TestTransitionGraphContextClosesBeforeOpeningVisible(t *testing.T) {
	active := 1
	maxActive := active
	var order []string
	if err := transitionGraphContext(func() error {
		order = append(order, "close")
		active--
		return nil
	}, func() error {
		order = append(order, "open")
		active++
		if active > maxActive {
			maxActive = active
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(order, ","), "close,open"; got != want {
		t.Fatalf("transition order = %q, want %q", got, want)
	}
	if maxActive != 1 {
		t.Fatalf("max simultaneous contexts = %d, want 1", maxActive)
	}
}

func TestTransitionGraphContextDoesNotOpenAfterCloseFailure(t *testing.T) {
	opened := false
	closeErr := errors.New("close failed")
	if err := transitionGraphContext(func() error { return closeErr }, func() error {
		opened = true
		return nil
	}); !errors.Is(err, closeErr) {
		t.Fatalf("transition error = %v, want close error", err)
	}
	if opened {
		t.Fatal("visible context opened after headless close failure")
	}
}

func TestGraphLifecycleReadyDoesNotOpenVisible(t *testing.T) {
	visibleCalls := 0
	persistCalls := 0
	err := runGraphLifecycle(time.Second,
		func() (string, error) { return "ready", nil },
		func() (string, error) { visibleCalls++; return "", nil },
		func() error { persistCalls++; return nil },
		func() error { t.Fatal("ready headless session must not request interaction"); return nil },
		func() bool { return false },
	)
	if err != nil {
		t.Fatal(err)
	}
	if visibleCalls != 0 || persistCalls != 1 {
		t.Fatalf("calls = visible:%d persist:%d, want 0 and 1", visibleCalls, persistCalls)
	}
}

func TestGraphRequestCandidateRejectsUntrustedFrameAndStoragePaths(t *testing.T) {
	validHeader := "Bearer token"
	if _, ok := graphRequestCandidate("token", "https://graph.microsoft.com/v1.0/me", "https://developer.microsoft.com/en-us/graph/graph-explorer", validHeader, false); ok {
		t.Fatal("subframe Graph request was accepted")
	}
	if _, ok := graphRequestCandidate("token", "https://graph.microsoft.com/v1.0/me", "https://developer.microsoft.com/en-us/graph/graph-explorer", validHeader, true); !ok {
		t.Fatal("main-frame Graph request was rejected")
	}
	if _, ok := graphRequestCandidate("token", "https://evil.example/graph.microsoft.com/v1.0/me", "https://developer.microsoft.com/en-us/graph/graph-explorer", validHeader, true); ok {
		t.Fatal("untrusted Graph host was accepted")
	}
	if _, ok := graphRequestCandidate("token", "https://graph.microsoft.com:443/v1.0/me", "https://developer.microsoft.com/en-us/graph/graph-explorer", validHeader, true); ok {
		t.Fatal("Graph host with explicit port was accepted")
	}
	// No localStorage/sessionStorage extraction path exists; only this request seam can create a candidate.
	if _, ok := graphRequestCandidate("token", "https://developer.microsoft.com/storage", "https://developer.microsoft.com/en-us/graph/graph-explorer", validHeader, true); ok {
		t.Fatal("storage-origin token was accepted")
	}
}

func TestGraphCandidateStaysQuarantinedUntilPersistence(t *testing.T) {
	captured := &tokens{}
	captureGraphCandidate(captured, "opaque-candidate", "https://graph.microsoft.com/v1.0/me", "https://developer.microsoft.com/en-us/graph/graph-explorer")
	captured.mu.Lock()
	defer captured.mu.Unlock()
	if captured.graphCandidate == nil {
		t.Fatal("candidate was not retained")
	}
	if captured.graphToken != "" {
		t.Fatal("unvalidated candidate escaped quarantine")
	}
}

func TestGraphInteractionRequiredIsNotFollowedByProviderFailure(t *testing.T) {
	withBatchEvents(t)
	err := runBatchJobSequence("graph", func() auth.TokenValues { return auth.TokenValues{} }, func(string) error {
		return errGraphInteractionRequired
	}, func(string, auth.TokenValues, map[string]string) (bool, error) { return false, nil }, func() string { return "" })
	if err == nil {
		t.Fatal("expected interaction-required failure")
	}
	if finishBatch(err, false) == 0 {
		t.Fatal("interaction-required failure returned success")
	}
	assertBatchEvents(t, decodeBatchEvents(t), []auth.AuthEvent{
		{Version: auth.EventVersion, BatchID: "batch", Provider: "graph", Generation: 1, Event: "interaction_required", State: "interaction_required", ErrorCode: "interaction_required"},
		{Version: auth.EventVersion, BatchID: "batch", Provider: "batch", Generation: 1, Event: "completed", State: "partial_failure", ErrorCode: "acquisition_failed"},
	})
}

func TestNormalizeBatchJobs(t *testing.T) {
	jobs, err := normalizeBatchJobs("web,notif,graph,web")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web_notif", "graph"}
	if len(jobs) != len(want) {
		t.Fatalf("got %v, want %v", jobs, want)
	}
	for i := range want {
		if jobs[i] != want[i] {
			t.Fatalf("got %v, want %v", jobs, want)
		}
	}
	if _, err := normalizeBatchJobs("fabric"); err == nil {
		t.Fatal("expected fabric to be rejected from the normal batch")
	}
}

func TestBatchJobCapturedRequiresFreshCredentials(t *testing.T) {
	before := tokenValuesForTest("old-graph", "old-web", "old-notif", "old-edu")
	after := tokenValuesForTest("new-graph", "new-web", "new-notif", "new-edu")
	if !batchJobCaptured("graph", before, after) || !batchJobCaptured("web_notif", before, after) || !batchJobCaptured("edu", before, after) {
		t.Fatal("expected each job to report a fresh credential")
	}
	if batchJobCaptured("graph", after, after) {
		t.Fatal("unchanged credentials must not complete a job")
	}
}

func TestBatchJobMissingProvidersKeepsPartialSuccess(t *testing.T) {
	before := tokenValuesForTest("old-graph", "old-web", "old-notif", "old-edu")
	after := tokenValuesForTest("old-graph", "new-web", "old-notif", "old-edu")
	missing := batchJobMissingProviders("web_notif", before, after)
	if len(missing) != 1 || missing[0] != "notif" {
		t.Fatalf("missing providers = %v, want [notif]", missing)
	}
	if batchJobCaptured("web_notif", before, after) {
		t.Fatal("partial web/notif capture must not report complete success")
	}
}

func tokenValuesForTest(graph, web, notif, edu string) auth.TokenValues {
	return auth.TokenValues{Graph: graph, Web: web, Notif: notif, EDU: edu}
}
