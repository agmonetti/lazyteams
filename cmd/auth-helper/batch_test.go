package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
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
	oldMode, oldWriter, oldID, oldStarted, oldGeneration, oldTerminal, oldWriteErr, oldPersist := eventMode, eventWriter, eventBatchID, eventStarted, eventGeneration, eventTerminal, eventWriteErr, persistBatchTokenValues
	var stream bytes.Buffer
	eventMode, eventWriter, eventBatchID, eventStarted, eventGeneration, eventTerminal, eventWriteErr = true, &stream, "batch", time.Now(), 1, false, nil
	persistBatchTokenValues = auth.PersistTokenValues
	t.Cleanup(func() {
		eventMode, eventWriter, eventBatchID, eventStarted, eventGeneration, eventTerminal, eventWriteErr, persistBatchTokenValues = oldMode, oldWriter, oldID, oldStarted, oldGeneration, oldTerminal, oldWriteErr, oldPersist
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

func TestGraphInteractionRequiredIsNotFollowedByProviderFailure(t *testing.T) {
	withBatchEvents(t)
	err := runBatchJobSequence("graph", func() auth.TokenValues { return auth.TokenValues{} }, func(string) error {
		return errGraphInteractionRequired
	}, func(string, auth.TokenValues, map[string]string) (bool, error) { return false, nil }, func() string { return "" })
	if err == nil {
		t.Fatal("expected interaction-required failure")
	}
	raw := batchEventBytes()
	if !bytes.Contains(raw, []byte(`"event":"interaction_required"`)) {
		t.Fatalf("missing interaction-required event: %s", raw)
	}
	if bytes.Contains(raw, []byte(`"event":"provider_failed"`)) {
		t.Fatalf("interaction-required was degraded to provider failure: %s", raw)
	}
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
