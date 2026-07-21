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
			{Key: "Enter", Description: "Open Thread (Cursor mode)"},
			{Key: "e / E", Description: "React / Edit (Cursor mode)"},
			{Key: "Del", Description: "Delete msg (Cursor mode)"},
			{Key: "i/r", Description: "Type message / Reply"},
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
}

// renderHelpMenu builds the cheat sheet UI dynamically from HelpData.
func renderHelpMenu() string {
	b := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0078D4"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Helper to format a category
	renderCategory := func(cat HelpCategory) string {
		var out strings.Builder
		out.WriteString(b.Render(cat.Title) + "\n\n")
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

	// Assuming HelpData has at least 4 items, let's split into 2 columns.
	half := len(HelpData) / 2
	if len(HelpData)%2 != 0 {
		half++
	}

	var col1Sections []string
	var col2Sections []string

	for i, cat := range HelpData {
		rendered := renderCategory(cat)
		if i < half {
			col1Sections = append(col1Sections, rendered, "\n")
		} else {
			col2Sections = append(col2Sections, rendered, "\n")
		}
	}

	col1 := lipgloss.JoinVertical(lipgloss.Left, col1Sections...)
	col2 := lipgloss.JoinVertical(lipgloss.Left, col2Sections...)

	table := lipgloss.JoinHorizontal(lipgloss.Top, col1, "    ", col2)

	return titleStyle.Render(" Cheat Sheet ") + "\n\n" + table + "\n\n" + dim.Render(" Press [Esc] or [?] to close")
}
