package graph

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestChatDisplayName(t *testing.T) {
	selfID := "self"
	selfChatID := "19:self_self@unq.gbl.spaces"

	tests := []struct {
		name string
		chat Chat
		want string
	}{
		{name: "topic", chat: Chat{Topic: "Study group"}, want: "Study group"},
		{name: "personal notes", chat: Chat{ID: selfChatID}, want: "Personal notes (You)"},
		{
			name: "members",
			chat: Chat{Members: []ChatMember{{UserID: selfID, DisplayName: "Me"}, {UserID: "other", DisplayName: "Ada"}}},
			want: "Ada",
		},
		{name: "legacy one on one", chat: Chat{ChatType: "oneOnOne"}, want: "1:1 Chat (Legacy)"},
		{name: "unnamed group", chat: Chat{ChatType: "group"}, want: "Unnamed chat (group)"},
		{name: "empty", chat: Chat{}, want: "Unnamed chat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.chat.DisplayName(selfID); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestActivityTypeLabel(t *testing.T) {
	for subtype, want := range map[string]string{
		"assignmentPublishedNotification": "[ASSIGN] ",
		"assignmentDueDateNotification":   "[DUE]    ",
		"mention":                         "[@]      ",
		"like":                            "[👍]     ",
		"reply":                           "[REPLY]  ",
		"channelMessage":                  "[MSG]    ",
		"unknown":                         "[NOTIF]  ",
	} {
		if got := ActivityTypeLabel(subtype); got != want {
			t.Errorf("ActivityTypeLabel(%q) = %q, want %q", subtype, got, want)
		}
	}
}

func TestAssignmentStatusLabel(t *testing.T) {
	tests := []struct {
		name string
		item Assignment
		want string
	}{
		{name: "submitted", item: Assignment{SubmissionStatus: "submitted"}, want: "[TURNED IN] "},
		{name: "returned", item: Assignment{SubmissionStatus: "returned"}, want: "[RETURNED]  "},
		{name: "revision", item: Assignment{SubmissionStatus: "reassigned"}, want: "[REVISION]  "},
		{name: "pending", item: Assignment{SubmissionStatus: "working", DueDateTime: time.Now().Add(time.Hour)}, want: "[PENDING]   "},
		{name: "overdue", item: Assignment{SubmissionStatus: "working", DueDateTime: time.Now().Add(-time.Hour)}, want: "[OVERDUE]   "},
		{name: "unknown", item: Assignment{}, want: "[ASSIGNMENT]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AssignmentStatusLabel(tt.item); got != tt.want {
				t.Errorf("AssignmentStatusLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeMailNickname(t *testing.T) {
	if got := sanitizeMailNickname("  Ingeniería 2026! "); got != "ingeniera2026" {
		t.Errorf("sanitizeMailNickname() = %q, want %q", got, "ingeniera2026")
	}
	if got := sanitizeMailNickname("!!!"); got != "team" {
		t.Errorf("empty nickname = %q, want team", got)
	}
	if got := sanitizeMailNickname(strings.Repeat("a", 70)); len(got) != 64 {
		t.Errorf("long nickname length = %d, want 64", len(got))
	}
}

func TestEncodeShareURL(t *testing.T) {
	raw := "HTTPS://Example.com/file?id=1"
	want := "u!" + strings.TrimRight(base64.URLEncoding.EncodeToString([]byte(strings.ToLower(raw))), "=")
	if got := encodeShareURL(raw); got != want {
		t.Errorf("encodeShareURL() = %q, want %q", got, want)
	}
}

func TestCleanHTML(t *testing.T) {
	got := cleanHTML("<p>Hello <strong>world</strong></p><a href=\"https://example.com\">link</a>")
	if !strings.Contains(got, "**world**") {
		t.Errorf("cleanHTML did not convert bold text: %q", got)
	}
	if !strings.Contains(got, "[link](https://example.com)") {
		t.Errorf("cleanHTML did not convert link: %q", got)
	}
}

func TestChatSvcError(t *testing.T) {
	err := (&ChatSvcError{StatusCode: 401, Message: "expired"}).Error()
	if err != "chatsvc error 401: expired" {
		t.Errorf("ChatSvcError.Error() = %q", err)
	}
}

func TestChannelRoleNumber(t *testing.T) {
	if n, ok := channelRoleNumber("Owner"); !ok || n != 2 {
		t.Errorf("Owner = %d/%v, want 2/true", n, ok)
	}
	if n, ok := channelRoleNumber("Member"); !ok || n != 1 {
		t.Errorf("Member = %d/%v, want 1/true", n, ok)
	}
	if _, ok := channelRoleNumber("Guest"); ok {
		t.Error("Guest should not be a valid role for the change-role API")
	}
}
