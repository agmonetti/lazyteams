package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
)

func TestPromptChoice(t *testing.T) {
	// 1. Valid choice in range
	in := bufio.NewScanner(strings.NewReader("2\n"))
	out := &bytes.Buffer{}
	choice, err := promptChoice(in, out, "> ", 1, 3)
	if err != nil || choice != 2 {
		t.Fatalf("choice = %d, err = %v", choice, err)
	}

	// 2. Out of range then valid
	in = bufio.NewScanner(strings.NewReader("5\n1\n"))
	out = &bytes.Buffer{}
	choice, err = promptChoice(in, out, "> ", 1, 3)
	if err != nil || choice != 1 {
		t.Fatalf("choice = %d, err = %v", choice, err)
	}
	if !strings.Contains(out.String(), "Opción inválida") {
		t.Fatalf("expected reprompt message in out: %s", out.String())
	}

	// 3. Empty input defaults to 1 when min=1 and max=1
	in = bufio.NewScanner(strings.NewReader("\n"))
	out = &bytes.Buffer{}
	choice, err = promptChoice(in, out, "> ", 1, 1)
	if err != nil || choice != 1 {
		t.Fatalf("choice = %d, err = %v", choice, err)
	}
}

func TestResolveTargetInteractive(t *testing.T) {
	// Mock Graph and ChatSvc responses
	mockTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()

		switch {
		case strings.Contains(url, "/me/joinedTeams"):
			body := `{"value":[{"id":"team-uuid-1","displayName":"Equipo A"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil

		case strings.Contains(url, "/channels"):
			body := `{"value":[
				{"id":"19:general@thread.tacv2","displayName":"General","membershipType":"standard"},
				{"id":"19:private1@thread.tacv2","displayName":"Canal Privado","membershipType":"private"}
			]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil

		case strings.Contains(url, "/threads/19:private1@thread.tacv2/members"):
			body := `{"members":[{"id":"8:orgid:user-uuid-1","role":"Admin"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil

		case strings.Contains(url, "/threads/19:private1@thread.tacv2?view=msnp24Equivalent"):
			body := `{"properties":{"substrateGroupId":"substrate-uuid-1"}}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil

		case strings.Contains(url, "/users/user-uuid-1"):
			body := `{"id":"user-uuid-1","displayName":"Agustin","mail":"test@example.com"}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil

		default:
			t.Fatalf("unexpected request: %s %s", req.Method, url)
			return nil, nil
		}
	})

	creds := &auth.CredentialSet{
		Web: auth.Credential{
			Token:  "dummy.web.token",
			Tenant: "tenant-uuid-99",
		},
		Graph: auth.Credential{
			Token:  "dummy.graph.token",
			Tenant: "tenant-uuid-99",
		},
	}
	client := graph.NewClient(creds)
	client.HTTPClient = &http.Client{Transport: mockTransport}

	// Single team, single private channel, single member -> auto-select or single choice
	in := strings.NewReader("1\n1\n1\n")
	out := &bytes.Buffer{}

	resolved, err := resolveTargetInteractive(client, in, out)
	if err != nil {
		t.Fatalf("resolveTargetInteractive error: %v", err)
	}

	if resolved.target.Team != "team-uuid-1" {
		t.Errorf("team = %q, want team-uuid-1", resolved.target.Team)
	}
	if resolved.target.Channel != "OID:substrate-uuid-1@tenant-uuid-99" {
		t.Errorf("channel = %q, want OID:substrate-uuid-1@tenant-uuid-99", resolved.target.Channel)
	}
	if resolved.target.User != "OID:user-uuid-1@tenant-uuid-99" {
		t.Errorf("user = %q, want OID:user-uuid-1@tenant-uuid-99", resolved.target.User)
	}
	// Current role was Admin (Owner), so target role must be 1 (Member)
	if resolved.target.Role != 1 {
		t.Errorf("role = %d, want 1", resolved.target.Role)
	}
	if resolved.threadID != "19:private1@thread.tacv2" {
		t.Errorf("threadID = %q, want 19:private1@thread.tacv2", resolved.threadID)
	}
	if resolved.auditUser != "8:orgid:user-uuid-1" {
		t.Errorf("auditUser = %q, want 8:orgid:user-uuid-1", resolved.auditUser)
	}

	// Verify that the audit can actually be parsed with this resolved auditUser
	audit, err := readAudit(context.Background(), client.HTTPClient, "dummy", resolved.threadID, resolved.auditUser)
	if err != nil || !audit.Exists || audit.Role != "Admin" {
		t.Fatalf("audit check with resolved target failed: %#v, %v", audit, err)
	}
}

func TestResolveTargetInteractiveNoPrivateChannels(t *testing.T) {
	mockTransport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()
		switch {
		case strings.Contains(url, "/me/joinedTeams"):
			body := `{"value":[{"id":"team-uuid-1","displayName":"Equipo A"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case strings.Contains(url, "/channels"):
			body := `{"value":[{"id":"19:general@thread.tacv2","displayName":"General","membershipType":"standard"}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, nil
		}
	})

	client := graph.NewClient(&auth.CredentialSet{
		Web: auth.Credential{Token: "dummy", Tenant: "tid"},
	})
	client.HTTPClient = &http.Client{Transport: mockTransport}

	in := strings.NewReader("1\n")
	out := &bytes.Buffer{}
	_, err := resolveTargetInteractive(client, in, out)
	if err == nil || !strings.Contains(err.Error(), "no tiene canales privados") {
		t.Fatalf("expected error about no private channels, got: %v", err)
	}
}
