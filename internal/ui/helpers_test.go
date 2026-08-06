package ui

import (
	"strings"
	"testing"
	"time"

	"teamsTUI/internal/graph"
)

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
