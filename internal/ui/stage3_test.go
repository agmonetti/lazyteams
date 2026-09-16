package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
)

type failTransport struct{}

func (failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &testTransportError{}
}

type countingTransport struct {
	calls atomic.Int32
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, &testTransportError{}
}

type testTransportError struct{}

func (*testTransportError) Error() string { return "unexpected request" }

func TestStage3PartialCredentialsAndPreflight(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{Graph: auth.Credential{Token: "graph"}})
	client.HTTPClient = &http.Client{Transport: failTransport{}}
	model := New(client, "", false)

	if got := model.capabilities[CapabilityGraph]; got != CapabilityReady {
		t.Fatalf("graph state = %q, want %q", got, CapabilityReady)
	}
	if got := model.capabilities[CapabilityEDU]; got != CapabilityMissing {
		t.Fatalf("edu state = %q, want %q", got, CapabilityMissing)
	}
	if err := requireCapability(client, CapabilityEDU); err == nil {
		t.Fatal("expected EDU preflight to reject missing credential")
	}
	if msg, ok := loadAssignmentsCmd(client)().(assignmentsErrMsg); !ok || msg.err == nil {
		t.Fatal("expected assignments command to stop before HTTP when EDU is absent")
	}
	if msg, ok := sendMessageCmd(client, "chat", "hello", nil, nil)().(messageSendErrMsg); !ok || msg.err == nil {
		t.Fatal("expected message command to stop before HTTP when Web is absent")
	}
}

func TestStage3PreflightCoversProviderSpecificCommands(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{Graph: auth.Credential{Token: "graph"}})
	client.HTTPClient = &http.Client{Transport: failTransport{}}

	if msg, ok := createChannelCmd(client, "team", "thread", "name", "standard")().(createChannelMsg); !ok || msg.err == nil {
		t.Fatal("expected Spaces preflight to stop channel creation")
	}
	if msg, ok := addChannelMemberCmd(client, "team", "thread", graph.TeamMember{ID: "user"})().(addChannelMemberMsg); !ok || msg.err == nil {
		t.Fatal("expected Fabric preflight to stop private-channel membership")
	}
	if msg, ok := editMessageCmd(client, "channel", "message", "edit")().(editMessageMsg); !ok || msg.err == nil {
		t.Fatal("expected Web preflight to stop message editing")
	}
	if msg := pollNotificationsCmd(client)(); msg != nil {
		t.Fatalf("missing Notif credential produced poll result %#v", msg)
	}
}

func TestStage3L052DependencyCombinations(t *testing.T) {
	t.Run("team members use Graph", func(t *testing.T) {
		transport := &countingTransport{}
		client := graph.NewClient(&auth.CredentialSet{Spaces: auth.Credential{Token: "spaces"}})
		client.HTTPClient = &http.Client{Transport: transport}
		if msg, ok := loadTeamMembersCmd(client, "team")().(teamMembersErrMsg); !ok || msg.err == nil {
			t.Fatal("expected Graph preflight to reject missing Graph credential")
		}
		if got := transport.calls.Load(); got != 0 {
			t.Fatalf("team members sent %d HTTP requests without Graph", got)
		}
	})

	for _, tc := range []struct {
		name string
		cmd  func(*graph.Client) tea.Msg
	}{
		{"add", func(client *graph.Client) tea.Msg {
			return addChannelMemberCmd(client, "team", "thread", graph.TeamMember{ID: "user"})()
		}},
		{"remove", func(client *graph.Client) tea.Msg {
			return removeChannelMemberCmd(client, "team", "thread", "user")()
		}},
		{"role", func(client *graph.Client) tea.Msg {
			return updateChannelMemberRoleCmd(client, "team", "thread", graph.TeamMember{ID: "user"}, "member")()
		}},
	} {
		t.Run("private channel mutations use Web and Fabric/"+tc.name, func(t *testing.T) {
			transport := &countingTransport{}
			client := graph.NewClient(&auth.CredentialSet{Fabric: auth.Credential{Token: "fabric"}})
			client.HTTPClient = &http.Client{Transport: transport}
			if msg := tc.cmd(client); msg == nil {
				t.Fatal("expected Web preflight to reject missing Web credential")
			}
			if got := transport.calls.Load(); got != 0 {
				t.Fatalf("private channel mutation sent %d HTTP requests without Web", got)
			}
		})
	}

	t.Run("assignment upload uses Graph and EDU", func(t *testing.T) {
		transport := &countingTransport{}
		client := graph.NewClient(&auth.CredentialSet{
			EDU:    auth.Credential{Token: "edu"},
			Cookie: auth.Credential{Token: "cookie"},
		})
		client.HTTPClient = &http.Client{Transport: transport}
		assignment := graph.Assignment{ID: "assignment"}
		if msg, ok := uploadAssignmentFileCmd(client, assignment, "file.txt")().(assignmentUploadDoneMsg); !ok || msg.err == nil {
			t.Fatal("expected Graph preflight to reject missing Graph credential")
		}
		if got := transport.calls.Load(); got != 0 {
			t.Fatalf("assignment upload sent %d HTTP requests without Graph", got)
		}
	})
}

func TestStage3EduFailureDoesNotBlockGraphData(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{
		Graph:  auth.Credential{Token: "graph"},
		EDU:    auth.Credential{Token: "edu"},
		Cookie: auth.Credential{Token: "cookie"},
	})
	model := New(client, "", false)
	updated, _ := model.Update(teamsMsg{teams: []graph.Team{{ID: "team"}}})
	model = updated.(Model)
	updated, _ = model.Update(chatsMsg{chats: []graph.Chat{{ID: "chat"}}})
	model = updated.(Model)
	updated, _ = model.Update(assignmentsErrMsg{err: &testTransportError{}})
	model = updated.(Model)

	if model.err != nil {
		t.Fatalf("module failure became fatal UI error: %v", model.err)
	}
	if !model.teamsLoaded || len(model.teams) != 1 || !model.chatsLoaded || len(model.chats) != 1 {
		t.Fatal("Graph teams/chats were not retained while EDU failed")
	}
	if model.capabilities[CapabilityEDU] != CapabilityUnknown {
		t.Fatalf("edu state = %q, want %q", model.capabilities[CapabilityEDU], CapabilityUnknown)
	}
	model.width, model.height = 120, 30
	view := model.View()
	if strings.Contains(view, "No teams found") || strings.Contains(view, "Press 'q' to quit") {
		t.Fatal("partial-capability model rendered a fatal/empty-account screen")
	}
}
