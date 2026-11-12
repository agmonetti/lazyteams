package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
)

func TestRestoreFailedThreadReplyDraftKeepsReplyMode(t *testing.T) {
	input := textarea.New()
	m := Model{input: input, failedDraft: &messageDraft{text: "reply", replyParentID: "parent", threadReply: true}}
	m.restoreFailedDraft()
	if m.input.Value() != "reply" || m.threadParentID != "parent" || !m.isReplyTyping || !m.showThread {
		t.Fatal("thread reply draft was not restored in reply mode")
	}
}

func TestRestoreFailedDraftRequiresExplicitRetry(t *testing.T) {
	input := textarea.New()
	m := Model{input: input, failedDraft: &messageDraft{
		text:   "keep me",
		images: []PendingImage{{Data: []byte("image"), ContentType: "image/png"}},
	}}
	m.restoreFailedDraft()
	if got := m.input.Value(); got != "keep me" {
		t.Fatalf("draft text = %q", got)
	}
	if len(m.pendingImages) != 1 || string(m.pendingImages[0].Data) != "image" {
		t.Fatal("draft images were not restored")
	}
	if !m.isTyping || !m.input.Focused() {
		t.Fatal("restored draft did not return to editing mode")
	}
	if m.failedDraft != nil {
		t.Fatal("draft remained queued for automatic replay")
	}
}

func TestThreadReplyFailureRestoresReplyThroughUpdate(t *testing.T) {
	input := textarea.New()
	input.SetValue("reply text")
	m := Model{
		input:          input,
		threadViewport: viewport.New(80, 10),
		threadParentID: "parent",
	}
	updated, _ := m.Update(threadReplySendErrMsg{err: fmt.Errorf("network timeout")})
	m = updated.(Model)
	if m.input.Value() != "reply text" || m.threadParentID != "parent" || !m.isReplyTyping || !m.showThread {
		t.Fatalf("reply state was not restored: text=%q parent=%q typing=%v thread=%v", m.input.Value(), m.threadParentID, m.isReplyTyping, m.showThread)
	}
	if !strings.Contains(m.threadViewport.View(), "Result may be uncertain") {
		t.Fatal("reply timeout did not warn about uncertain result")
	}
}

func TestMessageAndUploadFailuresWarnOnlyWhenResultIsUncertain(t *testing.T) {
	for _, test := range []struct {
		err         error
		wantWarning bool
	}{
		{fmt.Errorf("network unavailable"), true},
		{fmt.Errorf("upload timeout"), true},
		{fmt.Errorf("permission denied"), false},
	} {
		m := Model{
			input:       textarea.New(),
			viewport:    viewport.New(80, 10),
			failedDraft: &messageDraft{text: "keep me"},
		}
		updated, _ := m.Update(messageSendErrMsg{err: test.err})
		m = updated.(Model)
		content := m.viewport.View()
		if got := strings.Contains(content, "Result may be uncertain"); got != test.wantWarning {
			t.Errorf("error %q uncertainty warning = %v, want %v; content=%q", test.err, got, test.wantWarning, content)
		}
		if m.input.Value() != "keep me" {
			t.Fatalf("error %q did not restore draft", test.err)
		}
	}
}
