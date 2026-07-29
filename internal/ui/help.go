package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Shortcut represents a single keybinding and its description.
type Shortcut struct {
	Key         string
	Description string
}

// HelpCategory groups shortcuts under a specific section title.
type HelpCategory struct {
	Title     string
	Shortcuts []Shortcut
}

// HelpData is the centralized configuration for all application shortcuts.
// Modify this slice to add, remove, or change shortcuts globally.
var HelpData = []HelpCategory{
	{
		Title: "Global & Navigation",
		Shortcuts: []Shortcut{
			{Key: "1", Description: "Workspace: Teams"},
			{Key: "2", Description: "Workspace: DMs/Chats"},
			{Key: "3", Description: "Workspace: Activity"},
			{Key: "4", Description: "Workspace: Assignments"},
			{Key: "Tab", Description: "Toggle focus (Left/Right)"},
			{Key: "↑/↓", Description: "Navigate lists/scroll"},
			{Key: "g/G", Description: "Jump to top/bottom"},
			{Key: "Enter", Description: "Open selected item"},
			{Key: "p", Description: "Set Presence / Status"},
			{Key: "?", Description: "Toggle this help menu"},
			{Key: "q", Description: "Quit application"},
		},
	},
	{
		Title: "Workspaces (Left Panel)",
		Shortcuts: []Shortcut{
			{Key: "N", Description: "New Team"},
			{Key: "C", Description: "New Channel"},
			{Key: "n", Description: "New Direct Message"},
			{Key: "H", Description: "Hide/unhide team/channel"},
			{Key: "A", Description: "Toggle hidden teams/channels list"},
			{Key: "X/D", Description: "Delete Channel/Team"},
			{Key: "M", Description: "Manage Team Members"},
			{Key: "L", Description: "Copy link to item"},
		},
	},
	{
		Title: "Chat & Messages",
		Shortcuts: []Shortcut{
			{Key: "c", Description: "Toggle cursor mode (Select)"},
			{Key: "Enter", Description: "Thread (Teams) / Reply (DM cursor)"},
			{Key: "v", Description: "View/Download Image (Cursor mode)"},
			{Key: "e / E", Description: "React / Edit (Cursor mode)"},
			{Key: "Del", Description: "Delete msg (Cursor mode)"},
			{Key: "i/r", Description: "Type message / Thread Reply"},
			{Key: "Ctrl+P", Description: "Paste image from clipboard"},
			{Key: "/", Description: "Search in current chat"},
			{Key: "Esc", Description: "Clear search / Exit typing"},
			{Key: "f", Description: "View Files tab"},
			{Key: "I", Description: "View Info tab"},
			{Key: "u", Description: "Upload a file"},
		},
	},

	{
		Title: "Files & Drive",
		Shortcuts: []Shortcut{
			{Key: "Space", Description: "Select multiple files"},
			{Key: "v", Description: "Preview file"},
			{Key: "o", Description: "Download selected file(s)"},
			{Key: "u", Description: "Upload file to folder"},
			{Key: "F", Description: "New folder"},
			{Key: "Del", Description: "Delete file/folder"},
			{Key: "h/Esc", Description: "Back to parent folder"},
		},
	},
	{
		Title: "Assignments & Tasks",
		Shortcuts: []Shortcut{
			{Key: "Enter", Description: "View assignment details"},
			{Key: "j/k", Description: "Navigate files in detail view"},
			{Key: "Enter", Description: "Open file in browser"},
			{Key: "o", Description: "Download file"},
			{Key: "u", Description: "Upload file to assignment"},
			{Key: "Del", Description: "Remove uploaded file"},
			{Key: "s", Description: "Submit assignment"},
			{Key: "S", Description: "Undo turn in"},
		},
	},
}

// renderHelpMenu builds the cheat sheet UI dynamically from HelpData.
func renderHelpMenu() string {
	b := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0078D4"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Helper to format a category
	renderCategory := func(cat HelpCategory) string {
		var out strings.Builder
		out.WriteString(b.Render(cat.Title) + "\n")
		for _, s := range cat.Shortcuts {
			// Pad the key to align descriptions nicely.
			// Using lipgloss.Width to account for multi-byte runes like ↑ and ↓.
			paddedKey := fmt.Sprintf("[%s]", s.Key)
			w := lipgloss.Width(paddedKey)
			if w < 10 {
				paddedKey += strings.Repeat(" ", 10-w)
			}
			out.WriteString("  " + dim.Render(paddedKey) + " " + s.Description + "\n")
		}
		return out.String()
	}

	var col1Sections []string
	var col2Sections []string
	var col3Sections []string

	for i, cat := range HelpData {
		rendered := renderCategory(cat)
		if i == 0 || i == 1 { // Global & Navigation (11) + Workspaces (8)
			col1Sections = append(col1Sections, rendered)
			if i == 0 {
				col1Sections = append(col1Sections, "\n")
			}
		} else if i == 2 { // Chat & Messages (10)
			col2Sections = append(col2Sections, rendered)
		} else { // Files & Drive (7) + Assignments & Tasks (7)
			col3Sections = append(col3Sections, rendered)
			if i == 3 {
				col3Sections = append(col3Sections, "\n")
			}
		}
	}

	col1 := lipgloss.JoinVertical(lipgloss.Left, col1Sections...)
	col2 := lipgloss.JoinVertical(lipgloss.Left, col2Sections...)
	col3 := lipgloss.JoinVertical(lipgloss.Left, col3Sections...)

	table := lipgloss.JoinHorizontal(lipgloss.Top, col1, "    ", col2, "    ", col3)

	return titleStyle.Render(" Cheat Sheet ") + "\n\n" + table + "\n" + dim.Render(" Press [Esc] or [?] to close")
}
