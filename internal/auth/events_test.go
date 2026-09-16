package auth

import (
	"bytes"
	"testing"
)

func TestAuthEventRoundTripAndValidation(t *testing.T) {
	want := AuthEvent{Version: 1, Event: "credential_ready", BatchID: "b", Provider: "graph", Generation: 2, State: "ready"}
	var buf bytes.Buffer
	if err := WriteAuthEvent(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAuthEvent(bytes.TrimSpace(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAuthEventRejectsUnknownValues(t *testing.T) {
	if _, err := DecodeAuthEvent([]byte(`{"version":1,"event":"nope","batch_id":"b","provider":"graph","generation":0,"state":"ready"}`)); err == nil {
		t.Fatal("expected invalid event")
	}
	if _, err := DecodeAuthEvent([]byte(`{"version":1,"event":"provider_failed","batch_id":"b","provider":"graph","generation":1,"state":"unknown","error_code":"raw-library-error"}`)); err == nil {
		t.Fatal("expected invalid error code")
	}
}
