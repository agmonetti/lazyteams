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
			{Key: "Tab", Description: "Toggle focus"},
			{Key: "↑/↓", Description: "Navigate lists/scroll"},
			{Key: "g/G", Description: "Jump to top/bottom (End = latest message)"},
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
			{Key: "H", Description: "Hide/unhide item"},
			{Key: "A", Description: "Toggle hidden teams/channels list"},
			{Key: "X/D", Description: "Delete Channel/Team"},
			{Key: "M", Description: "Manage Team Members"},
			{Key: "L", Description: "Copy link to item"},
		},
	},
	{
		Title: "Chat & Messages",
		Shortcuts: []Shortcut{
			{Key: "c", Description: "Toggle cursor mode"},
			{Key: "o", Description: "Open link in browser (cursor mode)"},
			{Key: "Enter", Description: "Reply to thread/DM"},
			{Key: "v", Description: "View/Download Image"},
			{Key: "e / E", Description: "React / Edit"},
			{Key: "Del", Description: "Delete message"},
			{Key: "i/r", Description: "Type message"},
			{Key: "Ctrl+P", Description: "Paste clipboard image"},
			{Key: "/", Description: "Search chat"},
			{Key: "Esc", Description: "Clear search / exit"},
			{Key: "f", Description: "View Files tab"},
			{Key: "I", Description: "View Info tab"},
			{Key: "u", Description: "Upload a file"},
		},
	},
	{
		Title: "Channel Info (Private)",
		Shortcuts: []Shortcut{
			{Key: "a", Description: "Add channel member"},
			{Key: "x", Description: "Remove channel member"},
			{Key: "r", Description: "Change member role"},
			{Key: "Esc/h", Description: "Back to chat"},
		},
	},
	{
		Title: "Files & Drive",
		Shortcuts: []Shortcut{
			{Key: "Space", Description: "Select files"},
			{Key: "v", Description: "Preview file"},
			{Key: "o", Description: "Download selected"},
			{Key: "u", Description: "Upload to folder"},
			{Key: "F", Description: "New folder"},
			{Key: "Del", Description: "Delete file/folder"},
			{Key: "h/Esc", Description: "Back to parent"},
		},
	},
	{
		Title: "Activity",
		Shortcuts: []Shortcut{
			{Key: "Enter", Description: "View notification details"},
			{Key: "o", Description: "Go to channel"},
			{Key: "←/→", Description: "Filter notifications"},
			{Key: "r", Description: "Retry loading"},
		},
	},
	{
		Title: "Assignments & Tasks",
		Shortcuts: []Shortcut{
			{Key: "Enter", Description: "View details"},
			{Key: "j/k", Description: "Navigate files"},
			{Key: "Enter", Description: "Open file in browser"},
			{Key: "o", Description: "Download file"},
			{Key: "u", Description: "Upload file"},
			{Key: "Del", Description: "Remove upload"},
			{Key: "s", Description: "Submit assignment"},
			{Key: "S", Description: "Undo turn in"},
		},
	},
	{
		Title: "Mobile Mode",
		Shortcuts: []Shortcut{
			{Key: "Ctrl+B", Description: "Toggle mobile mode"},
			{Key: "Tab", Description: "Switch list/content"},
			{Key: "←/→", Description: "Filter tabs"},
			{Key: "1-4", Description: "Switch workspace"},
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

	for _, cat := range HelpData {
		rendered := renderCategory(cat)
		switch cat.Title {
		case "Global & Navigation", "Workspaces (Left Panel)":
			col1Sections = append(col1Sections, rendered)
			if cat.Title == "Global & Navigation" {
				col1Sections = append(col1Sections, "\n")
			}
		case "Chat & Messages", "Channel Info (Private)", "Activity":
			col2Sections = append(col2Sections, rendered)
		default:
			col3Sections = append(col3Sections, rendered)
			if cat.Title == "Files & Drive" {
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
