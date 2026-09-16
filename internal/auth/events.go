package auth

import (
	"encoding/json"
	"fmt"
	"io"
)

const EventVersion = 1

type AuthEvent struct {
	Version    int    `json:"version"`
	Event      string `json:"event"`
	BatchID    string `json:"batch_id"`
	Provider   string `json:"provider"`
	Generation uint64 `json:"generation"`
	State      string `json:"state"`
	DurationMS int64  `json:"duration_ms"`
	ErrorCode  string `json:"error_code"`
}

var eventNames = map[string]bool{
	"started": true, "credential_ready": true, "interaction_required": true,
	"provider_failed": true, "completed": true, "cancelled": true,
}

var eventProviders = map[string]bool{
	"batch": true, "graph": true, "web": true, "notif": true, "edu": true,
	"cookie": true, "spaces": true, "fabric": true,
}

var eventStates = map[string]bool{
	"running": true, "ready": true, "expired_or_revoked": true,
	"interaction_required": true, "permission_denied": true,
	"network_unavailable": true, "not_applicable": true, "unknown": true,
	"completed": true, "partial_failure": true, "cancelled": true,
}

var eventErrorCodes = map[string]bool{
	"": true, "interaction_required": true, "permission_denied": true,
	"network_unavailable": true, "not_applicable": true,
	"persistence_failed": true, "incompatible_credential": true,
	"stale_generation": true, "profile_in_use": true, "timeout": true,
	"cancelled": true, "protocol_error": true, "acquisition_failed": true,
}

func (e AuthEvent) Validate() error {
	if e.Version != EventVersion || !eventNames[e.Event] || !eventProviders[e.Provider] || !eventStates[e.State] {
		return fmt.Errorf("invalid auth event")
	}
	if e.BatchID == "" || e.DurationMS < 0 || !eventErrorCodes[e.ErrorCode] {
		return fmt.Errorf("invalid auth event fields")
	}
	return nil
}

func WriteAuthEvent(w io.Writer, e AuthEvent) error {
	if err := e.Validate(); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(e)
}

func DecodeAuthEvent(line []byte) (AuthEvent, error) {
	var e AuthEvent
	if len(line) > 64*1024 {
		return e, fmt.Errorf("auth event too large")
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return e, err
	}
	if err := e.Validate(); err != nil {
		return e, err
	}
	return e, nil
}
