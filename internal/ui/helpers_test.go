package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"lazyteams/internal/graph"
)

func TestMessageLink(t *testing.T) {
	tests := []struct {
		name string
		msg  graph.Message
		want string
	}{
		{
			name: "link attachment preferred",
			msg: graph.Message{
				Attachments: []graph.Attachment{
					{Name: "Form", URL: "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=1", Type: "link"},
				},
				Body: "See the form https://example.com/other",
			},
			want: "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=1",
		},
		{
			name: "markdown link in body",
			msg: graph.Message{
				Body: "Here: [docs](https://example.com/page)",
			},
			want: "https://example.com/page",
		},
		{
			name: "plain url in body",
			msg: graph.Message{
				Body: "Visit https://example.com/foo now",
			},
			want: "https://example.com/foo",
		},
		{
			name: "url stops at closing paren",
			msg: graph.Message{
				Body: "Go to https://example.com/a)b)",
			},
			want: "https://example.com/a",
		},
		{
			name: "forms url with underscore preserved",
			msg: graph.Message{
				Body: "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=0HlJNB3TV0yLoEka_0rK7X7PrFheVB5IhOuu9wZSryVUMEQwRURFUlc4VlpVVEtKVk9PNzAzNzcwVS4u",
			},
			want: "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=0HlJNB3TV0yLoEka_0rK7X7PrFheVB5IhOuu9wZSryVUMEQwRURFUlc4VlpVVEtKVk9PNzAzNzcwVS4u",
		},
		{
			name: "file attachment fallback",
			msg: graph.Message{
				Attachments: []graph.Attachment{
					{Name: "file.pdf", URL: "https://example.com/files/file.pdf", Type: "file"},
				},
				Body: "attachment",
			},
			want: "https://example.com/files/file.pdf",
		},
		{
			name: "no link",
			msg:  graph.Message{Body: "just text, no url"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageLink(tt.msg); got != tt.want {
				t.Errorf("messageLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterMessagesMatchesBodyAndSenderCaseInsensitively(t *testing.T) {
	m := Model{}
	messages := []graph.Message{
		{Body: "The exam is tomorrow", FromName: "Alice"},
		{Body: "Project notes", FromName: "Bob"},
		{Body: "Lunch plans", FromName: "Carla"},
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "body", query: "EXAM", want: 1},
		{name: "sender", query: "bob", want: 1},
		{name: "no match", query: "missing", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.filterMessages(messages, tt.query)
			if len(got) != tt.want {
				t.Fatalf("filterMessages(%q) returned %d messages, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestFilterMessagesInjectsSearchSentinels(t *testing.T) {
	m := Model{}
	messages := []graph.Message{
		{Body: "The exam is tomorrow", FromName: "Alice"},
		{Body: "Project notes", FromName: "Bob"},
	}

	got := m.filterMessages(messages, "exam")
	if len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	if got[0].Body != "The \x11exam\x12 is tomorrow" {
		t.Errorf("body did not get search sentinels: %q", got[0].Body)
	}

	got = m.filterMessages(messages, "bob")
	if len(got) != 1 {
		t.Fatalf("expected 1 sender match, got %d", len(got))
	}
	if got[0].FromName != "\x11Bob\x12" {
		t.Errorf("sender did not get search sentinels: %q", got[0].FromName)
	}
}

func TestFilterMessagesDoesNotMutateOriginal(t *testing.T) {
	m := Model{}
	original := []graph.Message{{Body: "The exam is tomorrow", FromName: "Alice"}}
	m.filterMessages(original, "exam")
	if original[0].Body != "The exam is tomorrow" {
		t.Error("filterMessages must not mutate the source messages")
	}
}

func TestCountSearchMatches(t *testing.T) {
	m := Model{}
	messages := []graph.Message{
		{Body: "The exam is tomorrow", FromName: "Alice"},
		{Body: "Project notes about exams", FromName: "Bob"},
		{Body: "Lunch plans", FromName: "Carla"},
	}
	if got := m.countSearchMatches(messages, "exam"); got != 2 {
		t.Errorf("countSearchMatches(exam) = %d, want 2", got)
	}
	if got := m.countSearchMatches(messages, "alice"); got != 1 {
		t.Errorf("countSearchMatches(alice) = %d, want 1", got)
	}
	if got := m.countSearchMatches(messages, "zzz"); got != 0 {
		t.Errorf("countSearchMatches(zzz) = %d, want 0", got)
	}
	if got := m.countSearchMatches(messages, ""); got != 0 {
		t.Errorf("countSearchMatches(empty) = %d, want 0", got)
	}
}

func TestHighlightSearchMatchesWrapsInBrackets(t *testing.T) {
	got := highlightSearchMatches("hello \x11world\x12!")
	if !strings.Contains(got, "[world]") {
		t.Errorf("highlightSearchMatches did not wrap match in brackets: %q", got)
	}
	if strings.Contains(got, "\x11") || strings.Contains(got, "\x12") {
		t.Errorf("highlightSearchMatches left sentinels behind: %q", got)
	}
}

func TestFilterMessagesReturnsOriginalSliceForEmptyQuery(t *testing.T) {
	m := Model{}
	messages := []graph.Message{{Body: "one"}, {Body: "two"}}
	got := m.filterMessages(messages, "")
	if len(got) != len(messages) {
		t.Fatalf("empty query returned %d messages, want %d", len(got), len(messages))
	}
	if &got[0] != &messages[0] {
		t.Error("empty query should return the original message slice")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "now", age: 10 * time.Second, want: "now"},
		{name: "one minute", age: 1*time.Minute + time.Second, want: "1 min ago"},
		{name: "minutes", age: 5*time.Minute + time.Second, want: "5 min ago"},
		{name: "one hour", age: time.Hour + time.Second, want: "1 h ago"},
		{name: "hours", age: 3*time.Hour + time.Second, want: "3 h ago"},
		{name: "one day", age: 24*time.Hour + time.Second, want: "1 day ago"},
		{name: "days", age: 3*24*time.Hour + time.Second, want: "3 days ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAge(time.Now().Add(-tt.age)); got != tt.want {
				t.Errorf("formatAge(%s) = %q, want %q", tt.age, got, tt.want)
			}
		})
	}
}

func TestResolveMentionsSortsNamesByLengthAndDeduplicates(t *testing.T) {
	mentions := resolveMentions("Hi @Ann and @Anna, then @Ann again", map[string]string{
		"Ann":  "mri-ann",
		"Anna": "mri-anna",
	})
	if len(mentions) != 2 {
		t.Fatalf("expected 2 mentions, got %d", len(mentions))
	}
	if mentions[0].DisplayName != "Anna" || mentions[1].DisplayName != "Ann" {
		t.Fatalf("mentions were not ordered longest-first: %#v", mentions)
	}
	if mentions[0].ItemID != 0 || mentions[1].ItemID != 1 {
		t.Errorf("unexpected mention item IDs: %#v", mentions)
	}
}

func TestNavigationSkipsHiddenTeamsAndChannels(t *testing.T) {
	teams := []graph.Team{{ID: "a"}, {ID: "hidden"}, {ID: "b"}}
	if got := nextVisibleTeam(teams, []string{"hidden"}, 0, false, 1); got != 2 {
		t.Errorf("next visible team = %d, want 2", got)
	}
	if got := nextVisibleTeam(teams, []string{"hidden"}, 0, true, 1); got != 1 {
		t.Errorf("next hidden team = %d, want 1", got)
	}

	channels := []graph.Channel{{ID: "a"}, {ID: "hidden"}, {ID: "b"}}
	if got := nextVisibleChannel(channels, []string{"hidden"}, 0, false, 1); got != 2 {
		t.Errorf("next visible channel = %d, want 2", got)
	}
	if got := nextVisibleChannel(channels, []string{"hidden"}, 0, true, 1); got != 1 {
		t.Errorf("next hidden channel = %d, want 1", got)
	}
}

func TestVisibleChatIndicesAndMembership(t *testing.T) {
	chats := []graph.Chat{
		{ID: "dm", ChatType: "oneOnOne"},
		{ID: "group", ChatType: "group"},
		{ID: "channel", ChatType: "channel"},
	}
	if got := visibleChatIndices(chats, false, true); len(got) != 1 || got[0] != 0 {
		t.Errorf("visible DMs = %v, want [0]", got)
	}
	if got := visibleChatIndices(chats, true, false); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("visible groups = %v, want [1 2]", got)
	}
	if !isInDMs(chats, 0) || isInDMs(chats, 1) {
		t.Error("isInDMs returned an incorrect result")
	}
	if !isInGroup(chats, 1) || isInGroup(chats, 0) {
		t.Error("isInGroup returned an incorrect result")
	}
	if isInDMs(chats, -1) || isInGroup(chats, len(chats)) {
		t.Error("out-of-range chat index should not belong to a section")
	}
}

func TestSortChatsPrioritizesPersonalUnreadAndRecent(t *testing.T) {
	chats := []graph.Chat{
		{ID: "group", ChatType: "group", LastUpdatedDateTime: "2026-08-05T12:00:00Z"},
		{ID: "unread", ChatType: "oneOnOne", LastUpdatedDateTime: "2026-08-05T10:00:00Z"},
		{ID: "self", ChatType: "oneOnOne", LastUpdatedDateTime: "2026-08-05T09:00:00Z"},
		{ID: "read", ChatType: "oneOnOne", LastUpdatedDateTime: "2026-08-05T11:00:00Z"},
	}
	sortChats(chats, map[string]bool{"unread": true}, "self")
	want := []string{"self", "unread", "read", "group"}
	for i, id := range want {
		if chats[i].ID != id {
			t.Errorf("chat %d = %q, want %q", i, chats[i].ID, id)
		}
	}
}

func TestGetAssignFileUsesReferenceFilesBeforeSubmittedFiles(t *testing.T) {
	a := graph.Assignment{
		RefFiles: []graph.AssignmentFile{{ID: "ref"}},
		MyFiles:  []graph.AssignmentFile{{ID: "mine"}},
	}
	if got := getAssignFile(a, 0); got == nil || got.ID != "ref" {
		t.Errorf("first assignment file = %#v, want ref", got)
	}
	if got := getAssignFile(a, 1); got == nil || got.ID != "mine" {
		t.Errorf("second assignment file = %#v, want mine", got)
	}
	if got := getAssignFile(a, 2); got != nil {
		t.Errorf("out-of-range assignment file = %#v, want nil", got)
	}
}

func TestSharePointHelpers(t *testing.T) {
	if !isSharePointURL("https://tenant.sharepoint.com/file.docx") {
		t.Error("SharePoint URL was not recognized")
	}
	if isSharePointURL("https://example.com/file.docx") {
		t.Error("unrelated URL was recognized as SharePoint")
	}
	got := buildSharePointViewerURL("https://tenant.sharepoint.com/sites/team/file.docx?download=1")
	want := "https://tenant.sharepoint.com/:b:/r/sites/team/file.docx?csf=1&download=1&web=1"
	if got != want {
		t.Errorf("viewer URL = %q, want %q", got, want)
	}
}

func TestSmallHelpers(t *testing.T) {
	for _, name := range []string{"README.md", "data.JSON", "main.go"} {
		if !isTextFile(name) {
			t.Errorf("isTextFile(%q) = false, want true", name)
		}
	}
	if isTextFile("image.png") {
		t.Error("PNG should not be treated as a text file")
	}
	if pluralS(1) != "" || pluralS(2) != "s" {
		t.Error("pluralS returned an unexpected suffix")
	}
	if got := remove([]string{"a", "b", "a"}, "a"); len(got) != 1 || got[0] != "b" {
		t.Errorf("remove returned %v, want [b]", got)
	}
	if indexOf([]int{3, 5, 8}, 5) != 1 || indexOf([]int{3, 5, 8}, 9) != -1 {
		t.Error("indexOf returned an unexpected result")
	}
}

func TestAppendFolderNodeDoesNotDuplicateCurrentNode(t *testing.T) {
	root := FolderNode{ID: "root", Name: "General"}
	child := FolderNode{ID: "child", Name: "Materials"}
	stack := appendFolderNode([]FolderNode{root, child}, child)
	if len(stack) != 2 {
		t.Fatalf("duplicate folder changed stack length to %d, want 2", len(stack))
	}
	stack = appendFolderNode(stack, FolderNode{ID: "other-drive", Name: "Materials", DriveID: "drive"})
	if len(stack) != 3 {
		t.Fatalf("different drive folder was not appended: %v", stack)
	}
}

func TestChannelRootMessageResetsStackAndIgnoresStaleChannel(t *testing.T) {
	m := Model{
		teams:         []graph.Team{{ID: "team-a"}},
		channels:      []graph.Channel{{ID: "channel-a"}},
		selectedTeam:  0,
		selectedChan:  0,
		folderStack:   []FolderNode{{ID: "old-root", Name: "General"}, {ID: "child", Name: "Materials"}},
		selectedFiles: make(map[int]bool),
	}
	updated, _ := m.Update(channelRootMsg{
		channelID: "channel-a",
		node:      FolderNode{ID: "root-a", Name: "General"},
	})
	got := updated.(Model)
	if len(got.folderStack) != 0 {
		t.Fatalf("root response did not normalize stack: %#v", got.folderStack)
	}

	updated, _ = got.Update(channelRootMsg{
		channelID: "channel-b",
		node:      FolderNode{ID: "root-b", Name: "Other"},
	})
	got = updated.(Model)
	if len(got.folderStack) != 0 {
		t.Fatalf("stale root response changed stack: %#v", got.folderStack)
	}
}

func TestUpdateNotificationsUsesServerReadStateAndPreservesSelection(t *testing.T) {
	m := Model{
		notifications: []graph.NotificationItem{
			{ID: "old", IsRead: false},
			{ID: "selected", IsRead: true},
		},
		selectedNotif:           1,
		notificationsRefreshing: true,
	}
	updateNotifications(&m, []graph.NotificationItem{
		{ID: "selected", IsRead: false},
		{ID: "new", IsRead: true},
	})

	if m.selectedNotif != 0 {
		t.Errorf("selected notification index = %d, want 0", m.selectedNotif)
	}
	if m.notifications[0].IsRead {
		t.Error("notification read state was not taken from the server response")
	}
	if m.notificationsRefreshing {
		t.Error("notification refresh should be marked complete")
	}
}

func TestUpdateNotificationsPreservesSelectionInFilteredList(t *testing.T) {
	m := Model{
		activityFilter: NotifFilterUnread,
		notifications: []graph.NotificationItem{
			{ID: "a", IsRead: false},
			{ID: "b", IsRead: true},
			{ID: "c", IsRead: false},
		},
		selectedNotif:           1, // points at "c" in the unread-filtered list
		notificationsRefreshing: true,
	}
	updateNotifications(&m, []graph.NotificationItem{
		{ID: "c", IsRead: false},
		{ID: "d", IsRead: false},
	})

	filtered := m.filteredNotifications()
	if m.selectedNotif < 0 || m.selectedNotif >= len(filtered) {
		t.Fatalf("selected notification index %d out of range [0,%d)", m.selectedNotif, len(filtered))
	}
	if got := filtered[m.selectedNotif].ID; got != "c" {
		t.Errorf("selected notification ID = %q, want %q (selection must follow the item across refreshes)", got, "c")
	}
}

func TestUpdateNotificationsClampsSelectionWhenItemDisappears(t *testing.T) {
	m := Model{
		activityFilter: NotifFilterUnread,
		notifications: []graph.NotificationItem{
			{ID: "a", IsRead: false},
			{ID: "b", IsRead: false},
		},
		selectedNotif: 1,
	}
	updateNotifications(&m, []graph.NotificationItem{
		{ID: "z", IsRead: true},
	})

	filtered := m.filteredNotifications()
	if len(filtered) != 0 {
		t.Fatalf("expected empty filtered list, got %d items", len(filtered))
	}
	if m.selectedNotif != 0 {
		t.Errorf("selected notification index = %d, want 0 when the filtered list is empty", m.selectedNotif)
	}
}

func TestUpdateChannelMemberRoleUpdatesOptimistically(t *testing.T) {
	m := Model{
		channelMembers: []graph.TeamMember{{ID: "u1", Role: "Member"}, {ID: "u2", Role: "Owner"}},
	}
	updated, _ := m.Update(updateChannelMemberRoleMsg{userID: "u1", role: "Owner"})
	got := updated.(Model)
	if len(got.channelMembers) != 2 || got.channelMembers[0].Role != "Owner" {
		t.Fatalf("role was not updated optimistically: %#v", got.channelMembers)
	}
	if got.channelRoleErr != "" {
		t.Errorf("channelRoleErr should be empty on success, got %q", got.channelRoleErr)
	}
	if got.downloadStatus == "" {
		t.Error("success should set a download status message")
	}
}

func TestUpdateChannelMemberRoleErrorIsPersistent(t *testing.T) {
	m := Model{
		channelMembers: []graph.TeamMember{{ID: "u1", Role: "Owner"}},
	}
	updated, _ := m.Update(updateChannelMemberRoleMsg{
		err:    fmt.Errorf("change channel member role error 400: cannot demote the last owner"),
		userID: "u1",
		role:   "Member",
	})
	got := updated.(Model)
	if got.channelRoleErr == "" {
		t.Fatal("server error should be stored in channelRoleErr")
	}
	if got.channelMembers[0].Role != "Owner" {
		t.Error("failed role change must not alter the local member list")
	}
}

func TestIsLastChannelOwner(t *testing.T) {
	owner := graph.TeamMember{ID: "u1", Role: "Owner"}
	member := graph.TeamMember{ID: "u2", Role: "Member"}

	if !isLastChannelOwner([]graph.TeamMember{owner, member}, "u1") {
		t.Error("single owner that is selected should be the last owner")
	}
	if isLastChannelOwner([]graph.TeamMember{owner, member}, "u2") {
		t.Error("a member must never be considered the last owner")
	}
	if isLastChannelOwner([]graph.TeamMember{
		{ID: "u1", Role: "Owner"},
		{ID: "u3", Role: "Owner"},
	}, "u1") {
		t.Error("with two owners neither is the last owner")
	}
	if isLastChannelOwner(nil, "u1") {
		t.Error("empty member list must not report a last owner")
	}
}

func TestChangeChannelRoleBlocksDemotingLastOwner(t *testing.T) {
	m := Model{
		channelMembers:             []graph.TeamMember{{ID: "u1", Role: "Owner"}, {ID: "u2", Role: "Member"}},
		showChangeChannelRolePopup: true,
		changeChannelRoleCursor:    1, // "Member"
		channelMemberCursor:        0, // the owner
		teams:                      []graph.Team{{ID: "team-a"}},
		channels:                   []graph.Channel{{ID: "channel-a"}},
	}
	updated, cmd, _ := m.handleChangeChannelRolePopup(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.showChangeChannelRolePopup {
		t.Error("popup should close after a blocked demotion")
	}
	if got.channelRoleErr == "" {
		t.Error("blocked demotion should set a persistent channelRoleErr")
	}
	if got.channelMembers[0].Role != "Owner" {
		t.Error("blocked demotion must not alter the local member list")
	}
	if cmd != nil {
		t.Error("blocked demotion must not issue a network command")
	}
}

func TestChangeChannelRoleAllowsDemotingNonLastOwner(t *testing.T) {
	m := Model{
		channelMembers: []graph.TeamMember{
			{ID: "u1", Role: "Owner"},
			{ID: "u2", Role: "Owner"},
			{ID: "u3", Role: "Member"},
		},
		showChangeChannelRolePopup: true,
		changeChannelRoleCursor:    1, // "Member"
		channelMemberCursor:        0, // first owner (not last)
		teams:                      []graph.Team{{ID: "team-a"}},
		channels:                   []graph.Channel{{ID: "channel-a"}},
	}
	_, cmd, _ := m.handleChangeChannelRolePopup(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("demoting a non-last owner should issue the update command")
	}
}

func TestClearDownloadStatusRefreshesInfoView(t *testing.T) {
	m := Model{
		viewMode:         ModeInfo,
		channelInfo:      &graph.Channel{ID: "channel-a", DisplayName: "General"},
		channelMembers:   []graph.TeamMember{{ID: "u1", Role: "Owner"}},
		downloadStatus:   "✓ Role updated to Member",
		downloadStatusID: 5,
	}
	updated, _ := m.Update(clearDownloadStatusMsg{id: 5})
	got := updated.(Model)
	if got.downloadStatus != "" {
		t.Error("matching clear message should empty downloadStatus")
	}
}

func TestFormatDownloadResultsKeepsEachResultVisible(t *testing.T) {
	got := formatDownloadResults([]string{
		"✓ first.pdf → /tmp/first.pdf",
		"✓ second.docx → /tmp/second.docx",
	})
	want := "✓ first.pdf → /tmp/first.pdf\n✓ second.docx → /tmp/second.docx"
	if got != want {
		t.Errorf("formatDownloadResults() = %q, want %q", got, want)
	}
}

func TestDownloadStatusIndentsEveryResultLine(t *testing.T) {
	status := "first result\nsecond result\nthird result"
	want := "first result\n  second result\n  third result"
	got := strings.ReplaceAll(status, "\n", "\n  ")
	if got != want {
		t.Errorf("indented download status = %q, want %q", got, want)
	}
}

func TestIsTextFile(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{"README.md", true},
		{"data.json", true},
		{"image.png", false},
		{"archive.zip", false},
		{"SCRIPT.GO", true}, // case insensitive
		{"noextension", false},
		{"doc.js0n", false},
		{"file1.npm", false},
		{"file.os.txt", true},
		{"file.txts", false},
		{"file.docx", false},
		{"file.rar", false},
		{"", false},
		{".gitignore", false},
		{"file.go.bak", false},
	}

	for _, tc := range cases {
		got := isTextFile(tc.input)
		if got != tc.expected {
			t.Errorf("isTextFile(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestBuildSharePointViewerURL(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			// normal case
			input:    "https://tenant.sharepoint.com/sites/team/file.docx",
			expected: "https://tenant.sharepoint.com/:b:/r/sites/team/file.docx?csf=1&web=1",
		},
		{
			// existing query params stay
			input:    "https://tenant.sharepoint.com/sites/team/file.docx?version=2",
			expected: "https://tenant.sharepoint.com/:b:/r/sites/team/file.docx?csf=1&version=2&web=1",
		},
		{
			//invalid URL → same URL
			input:    "://invalid",
			expected: "://invalid",
		},
		{
			// empty URL → empty URL
			input:    "",
			expected: "",
		},
	}

	for _, tc := range cases {
		got := buildSharePointViewerURL(tc.input)
		if got != tc.expected {
			t.Errorf("buildSharePointViewerURL(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.expected)
		}
	}
}
func TestIsSharePointURL(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{"sHarepoint.com", true},
		{"SHAREPOINT.COM", true},
		{"sharepo1nt.com", false},
		{"sharepoint.live.com", false},
		{"onedrive.live.com", true},
		{"onedrive.com", false},
		{"onedrive.sharepoint.com", true},
		{"", false},
		{"drive.live.com", false},
	}

	for _, tc := range cases {
		got := isSharePointURL(tc.input)
		if got != tc.expected {
			t.Errorf("isSharePointURL(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestPluralS(t *testing.T) {
	cases := []struct {
		input    int
		expected string
	}{
		{1, ""},
		{0, "s"},
		{2, "s"},
		{-1, "s"},
	}
	for _, tc := range cases {
		got := pluralS(tc.input)
		if got != tc.expected {
			t.Errorf("pluralS(%d) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Alice Smith", "Alice Smith"},
		{"esc sequence", "Alice\x1b[1mSmith", "Alice[1mSmith"},
		{"osctl title", "\x1b]0;evil\x07", "]0;evil"},
		{"newline and tab", "A\nB\tC", "ABC"},
		{"bell and backspace", "a\x07b\x08c", "abc"},
		{"c1 bytes", "\u009bhi", "hi"},
		{"search sentinels preserved", "x\x11match\x12y", "x\x11match\x12y"},
		{"mention sentinels preserved", "@\x1EAl\x1F", "@\x1EAl\x1F"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeName(tt.in); got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMakeClickableLinkPreservesStyling(t *testing.T) {
	// Styled text must pass through intact: renderFilesContent passes a
	// lipgloss-styled line here, and stripping ESC would destroy the styling.
	styled := "\x1b[1mfile.txt\x1b[0m"
	got := makeClickableLink(styled, "https://example.com")
	if !strings.Contains(got, styled) {
		t.Errorf("makeClickableLink stripped styling from text: %q", got)
	}
	// The URL is still stripped of escape characters.
	gotURL := makeClickableLink("link", "https://example.com/\x1b]0;x\x07")
	if strings.Contains(gotURL, "\x1b]0;") {
		t.Errorf("makeClickableLink allowed OSC injection in URL: %q", gotURL)
	}
}

// TestMakeClickableLinkStripsURLControlBytes ensures no C0/C1 control byte
// survives into the URL embedded in the OSC 8 hyperlink.
func TestMakeClickableLinkStripsURLControlBytes(t *testing.T) {
	payload := "\x1b]0;evil\x07" + "\x9b1;2" + "\x1b[31m" + "\x00" + "\x1c"
	url := "https://example.com/" + payload
	got := makeClickableLink("link", url)

	const oscPrefix = "\x1b]8;;"
	start := strings.Index(got, oscPrefix)
	if start == -1 {
		t.Fatalf("no OSC 8 link found in output: %q", got)
	}
	rest := got[start+len(oscPrefix):]
	end := strings.Index(rest, "\x1b\\")
	if end == -1 {
		t.Fatalf("no OSC 8 terminator found in output: %q", got)
	}
	embedded := rest[:end]

	if !strings.Contains(embedded, "https://example.com/") {
		t.Errorf("embedded URL lost its content: %q", embedded)
	}
	for _, bad := range []string{"\x1b", "\x07", "\x9b", "\x00", "\x1c", "\x1f"} {
		if strings.Contains(embedded, bad) {
			t.Errorf("makeClickableLink left control byte %q in embedded URL %q", bad, embedded)
		}
	}
}
