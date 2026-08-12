package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"lazyteams/internal/graph"

	"github.com/charmbracelet/lipgloss"
)

func TestRootMessages(t *testing.T) {
	msgs := []graph.Message{
		{ID: "r1", RootMessageID: "r1", MessageType: "Text"},
		{ID: "r2", RootMessageID: "", MessageType: "RichText/Html"},
		{ID: "activity", RootMessageID: "activity", MessageType: "ThreadActivity/MemberJoined"},
		{ID: "reply", RootMessageID: "r1", MessageType: "Text"},
	}

	got := rootMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("rootMessages returned %d roots, want 2: %#v", len(got), got)
	}
	if got[0].ID != "r1" || got[1].ID != "r2" {
		t.Errorf("rootMessages = [%s, %s], want [r1, r2]", got[0].ID, got[1].ID)
	}
}

func TestValidDMMessages(t *testing.T) {
	msgs := []graph.Message{
		{ID: "text", MessageType: "Text"},
		{ID: "html", MessageType: "RichText/Html"},
		{ID: "joined", MessageType: "ThreadActivity/MemberJoined"},
		{ID: "left", MessageType: "ThreadActivity/MemberLeft"},
		{ID: "topic", MessageType: "ThreadActivity/TopicUpdate"},
		{ID: "add", MessageType: "ThreadActivity/AddMember"},
		{ID: "del", MessageType: "ThreadActivity/DeleteMember"},
		{ID: "deluser", MessageType: "ThreadActivity/DeleteUser"},
	}

	got := validDMMessages(msgs)
	if len(got) != 2 {
		t.Fatalf("validDMMessages returned %d messages, want 2: %#v", len(got), got)
	}
	if got[0].ID != "text" || got[1].ID != "html" {
		t.Errorf("validDMMessages = [%s, %s], want [text, html]", got[0].ID, got[1].ID)
	}
}

func TestRepliesForSortsByCreatedAtAscending(t *testing.T) {
	t1 := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	msgs := []graph.Message{
		{ID: "p", RootMessageID: "p", CreatedAt: t1},
		{ID: "r-late", RootMessageID: "p", CreatedAt: t3},
		{ID: "r-mid", RootMessageID: "p", CreatedAt: t2},
		{ID: "r-early", RootMessageID: "p", CreatedAt: t1},
		{ID: "other", RootMessageID: "x", CreatedAt: t1},
	}

	got := repliesFor(msgs, "p")
	want := []string{"r-early", "r-mid", "r-late"}
	if len(got) != len(want) {
		t.Fatalf("repliesFor returned %d replies, want %d: %#v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("repliesFor[%d].ID = %q, want %q (must be sorted ascending by CreatedAt)", i, got[i].ID, id)
		}
	}
}

func TestReplyCount(t *testing.T) {
	msgs := []graph.Message{
		{ID: "p", RootMessageID: "p"},
		{ID: "r1", RootMessageID: "p"},
		{ID: "r2", RootMessageID: "p"},
		{ID: "other", RootMessageID: "x"},
	}
	if got := replyCount(msgs, "p"); got != 2 {
		t.Errorf("replyCount = %d, want 2", got)
	}
	if got := replyCount(msgs, "missing"); got != 0 {
		t.Errorf("replyCount(missing) = %d, want 0", got)
	}
}

func TestGetOptimalWrapWidth(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		maxW int
		want int
	}{
		{"all narrow", "short line", 40, 0},
		{"multiline narrow", "a\nbb\nccc", 40, 0},
		{"single line wider than max", strings.Repeat("x", 60), 30, 30},
		{"widest line matches max", strings.Repeat("x", 30), 30, 30},
		{"multiline with one wide line", "hello\n" + strings.Repeat("x", 50), 20, 20},
		{"empty", "", 40, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getOptimalWrapWidth(tt.raw, tt.maxW); got != tt.want {
				t.Errorf("getOptimalWrapWidth(%q, %d) = %d, want %d", tt.raw, tt.maxW, got, tt.want)
			}
		})
	}
}

func TestCleanHTMLForEdit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"paragraph tags", "<p>Hello</p>", "Hello"},
		{"line break", "<p>a<br>b</p>", "a\nb"},
		{"entities", "&amp; &lt; &gt; &nbsp;", "& < >"},
		{"mention", `<p><span itemtype="http://schema.skype.com/Mention" itemid="8:orgid:1">Alice</span></p>`, "@Alice"},
		{"mixed", `<p>Hi <span itemtype="http://schema.skype.com/Mention">Bob</span><br>bye &amp; ciao</p>`, "Hi @Bob\nbye & ciao"},
		{"trims surrounding space", "  <p>Hi</p>  ", "Hi"},
		{"mention with @ in span", `<span itemtype="http://schema.skype.com/Mention">@Bob</span>`, "@Bob"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanHTMLForEdit(tt.in); got != tt.want {
				t.Errorf("cleanHTMLForEdit(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRenderMarkdownWrapsLongURL(t *testing.T) {
	const url = "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=0HlJNB3TV0yLoEka_0rK7X7PrFheVB5IhOuu9wZSryVUMEQwRURFUlc4VlpVVEtKVk9PNzAzNzcwVS4u"

	for _, width := range []int{49, 60, 90} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			out := renderMarkdown(url, width)
			for i, line := range strings.Split(out, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("line %d width = %d > %d: %q", i, w, width, line)
				}
			}
			reconstructed := ansiToken.ReplaceAllString(out, "")
			reconstructed = strings.ReplaceAll(reconstructed, " ", "")
			reconstructed = strings.ReplaceAll(reconstructed, "\n", "")
			if reconstructed != url {
				t.Errorf("URL not fully preserved after wrapping:\n got: %q\nwant: %q", reconstructed, url)
			}
		})
	}
}

func TestRenderMarkdownWrapsLongURLNeverBreaksAtUnderscore(t *testing.T) {
	const url = "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=0HlJNB3TV0yLoEka_0rK7X7PrFheVB5IhOuu9wZSryVUMEQwRURFUlc4VlpVVEtKVk9PNzAzNzcwVS4u"

	out := renderMarkdown(url, 60)
	for i, line := range strings.Split(out, "\n") {
		clean := ansiToken.ReplaceAllString(line, "")
		if strings.HasSuffix(clean, "_") {
			t.Errorf("line %d ends with an underscore: %q", i, clean)
		}
	}
}

func TestWrapAnsiPreservesUTF8(t *testing.T) {
	const emoji = "🚀"
	content := "hello " + emoji + " " + "https://example.com/aaa/bbb-" + strings.Repeat("x", 40) + " bye"

	out := wrapAnsi(content, 30)
	reconstructed := ansiToken.ReplaceAllString(out, "")
	reconstructed = strings.ReplaceAll(reconstructed, "\n", "")
	reconstructed = strings.ReplaceAll(reconstructed, " ", "")
	if reconstructed != strings.ReplaceAll(content, " ", "") {
		t.Errorf("wrapAnsi corrupted UTF-8:\n got: %q\nwant: %q", reconstructed, strings.ReplaceAll(content, " ", ""))
	}
	if !strings.Contains(out, emoji) {
		t.Errorf("emoji not preserved as a whole unit:\n%q", out)
	}
}

func TestWrapAnsiNormalizesCRLF(t *testing.T) {
	out := wrapAnsi("first line\r\nsecond line\r\nthird line", 40)
	for i, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "\r") {
			t.Errorf("line %d still contains a carriage return: %q", i, line)
		}
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("wrapAnsi should keep the two line breaks, got %d: %q", strings.Count(out, "\n"), out)
	}
}

func TestWrapAnsiPreservesIndentation(t *testing.T) {
	content := "  " + strings.Repeat("palabra ", 20)
	out := wrapAnsi(content, 40)
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapping, got a single line: %q", out)
	}
	for i, line := range lines {
		clean := ansiToken.ReplaceAllString(line, "")
		if !strings.HasPrefix(clean, "  ") {
			t.Errorf("line %d lost its indentation: %q", i, clean)
		}
		if w := lipgloss.Width(clean); w > 40 {
			t.Errorf("line %d width = %d > 40: %q", i, w, clean)
		}
	}
}

func TestWrapAnsiPreservesIndentedContent(t *testing.T) {
	content := "  " + strings.Repeat("ab cd ", 10)
	out := wrapAnsi(content, 20)
	got := ansiToken.ReplaceAllString(out, "")
	got = strings.ReplaceAll(got, "\n", " ")
	got = strings.ReplaceAll(got, " ", "")
	want := strings.ReplaceAll(content, " ", "")
	if got != want {
		t.Errorf("wrapAnsi altered indented content:\n got: %q\nwant: %q", got, want)
	}
}

func TestWrapAnsiPreservesIndentationWithLongURL(t *testing.T) {
	const url = "https://forms.cloud.microsoft/Pages/ResponsePage.aspx?id=0HlJNB3TV0yLoEka_0rK7X7PrFheVB5IhOuu9wZSryVUMEQwRURFUlc4VlpVVEtKVk9PNzAzNzcwVS4u"
	content := "  " + url

	out := wrapAnsi(content, 60)
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		clean := ansiToken.ReplaceAllString(line, "")
		if !strings.HasPrefix(clean, "  ") {
			t.Errorf("line %d lost its indentation: %q", i, clean)
		}
		if w := lipgloss.Width(clean); w > 60 {
			t.Errorf("line %d width = %d > 60: %q", i, w, clean)
		}
	}
	reconstructed := ansiToken.ReplaceAllString(out, "")
	reconstructed = strings.ReplaceAll(reconstructed, " ", "")
	reconstructed = strings.ReplaceAll(reconstructed, "\n", "")
	if reconstructed != url {
		t.Errorf("URL not fully preserved with indentation:\n got: %q\nwant: %q", reconstructed, url)
	}
}

func TestHighlightMentions(t *testing.T) {
	if got := highlightMentions("plain text"); got != "plain text" {
		t.Errorf("highlightMentions without sentinels = %q, want unchanged", got)
	}

	out := highlightMentions("Hi \x1E@Alice\x1F!")
	if !strings.Contains(out, "@Alice") {
		t.Errorf("highlightMentions lost the mention text: %q", out)
	}
	if strings.Contains(out, "\x1E") || strings.Contains(out, "\x1F") {
		t.Errorf("highlightMentions left sentinels in place: %q", out)
	}
}
