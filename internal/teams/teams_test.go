package teams

import (
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/graph"
)

func TestAggregateChatAttachmentsDeduplicatesAndSortsNewestFirst(t *testing.T) {
	old := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC)
	newer := old.Add(2 * time.Hour)

	items := AggregateChatAttachments([]graph.Message{
		{
			CreatedAt: old,
			Attachments: []graph.Attachment{
				{Name: "old.pdf", URL: "https://files.example/old.pdf", Type: "file"},
				{Name: "shared", URL: "https://example.com/shared", Type: "link"},
			},
		},
		{
			CreatedAt: newer,
			Body:      "See https://example.com/new for details and https://example.com/shared again.",
			Attachments: []graph.Attachment{
				{Name: "new.docx", URL: "https://files.example/new.docx", Type: "file"},
				{Name: "duplicate", URL: "https://files.example/old.pdf", Type: "file"},
			},
		},
	})

	if len(items) != 4 {
		t.Fatalf("expected 4 unique attachments, got %d", len(items))
	}
	// Attachments are intentionally ordered by their parent message timestamp,
	// newest first; this is the user-visible ordering contract of the helper.
	wantURLs := []string{
		"https://files.example/new.docx",
		"https://example.com/new",
		"https://files.example/old.pdf",
		"https://example.com/shared",
	}
	for i, want := range wantURLs {
		if items[i].WebUrl != want {
			t.Errorf("item %d: expected URL %q, got %q", i, want, items[i].WebUrl)
		}
	}
	if !items[1].IsExternalLink || !items[3].IsExternalLink {
		t.Error("body and link attachments should be marked as external links")
	}
}

func TestAggregateChatAttachmentsSkipsEmptyURLs(t *testing.T) {
	items := AggregateChatAttachments([]graph.Message{{
		Attachments: []graph.Attachment{{Name: "empty", URL: ""}},
	}})
	if len(items) != 0 {
		t.Fatalf("expected empty URL attachment to be skipped, got %d items", len(items))
	}
}

func TestGetFileIcon(t *testing.T) {
	folder := &struct {
		ChildCount int `json:"childCount"`
	}{ChildCount: 1}
	tests := []struct {
		name string
		item graph.DriveItem
		want string
	}{
		{"external link", graph.DriveItem{IsExternalLink: true}, "[LINK]"},
		{"folder", graph.DriveItem{Folder: folder}, "[DIR]"},
		{"pdf", graph.DriveItem{Name: "REPORT.PDF"}, "[PDF]"},
		{"presentation", graph.DriveItem{Name: "slides.pptx"}, "[PPT]"},
		{"document", graph.DriveItem{Name: "notes.docx"}, "[DOC]"},
		{"spreadsheet", graph.DriveItem{Name: "data.xlsx"}, "[XLS]"},
		{"video", graph.DriveItem{Name: "clip.mkv"}, "[VID]"},
		{"archive", graph.DriveItem{Name: "bundle.zip"}, "[ZIP]"},
		{"unknown", graph.DriveItem{Name: "file.bin"}, "[FILE]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetFileIcon(tt.item); got != tt.want {
				t.Errorf("GetFileIcon() = %q, want %q", got, tt.want)
			}
		})
	}
}
