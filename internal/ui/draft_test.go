package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
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
