package directorypicker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Calculate popup dimensions (80% of screen, max 80x24)
	popupW := m.width * 80 / 100
	if popupW > 80 {
		popupW = 80
	}
	if popupW < 60 {
		popupW = 60
	}
	popupH := m.height * 70 / 100
	if popupH > 24 {
		popupH = 24
	}
	if popupH < 16 {
		popupH = 16
	}

	// Left panel: quick access (30% of popup width)
	leftW := popupW * 30 / 100
	rightW := popupW - leftW - 4 // account for borders and gap

	// Build quick access panel
	qaLines := make([]string, 0, len(m.quickAccess))
	for i, item := range m.quickAccess {
		cursor := "  "
		style := normalStyle
		if i == m.quickCursor && m.onQuickPanel {
			cursor = "▶ "
			style = selectedStyle
		}
		icon := "📁"
		if item.Name == "Home" {
			icon = "🏠"
		} else if item.Name == "Downloads" {
			icon = "📄"
		} else if item.Name == "Documents" {
			icon = "📝"
		} else if item.Name == "Desktop" {
			icon = "🖥"
		}
		qaLines = append(qaLines, fmt.Sprintf("%s%s %s", cursor, icon, style.Render(item.Name)))
	}
	qaContent := strings.Join(qaLines, "\n")
	qaPanel := quickAccessStyle.Width(leftW).Render(titleStyle.Render("Quick Access") + "\n" + qaContent)

	// Build directory/file list
	dirLines := make([]string, 0, len(m.entries)+1)
	// Add parent directory entry
	dirLines = append(dirLines, fmt.Sprintf("  📁 %s", dimStyle.Render("..")))

	for i, entry := range m.entries {
		cursor := "  "
		style := normalStyle
		if i == m.cursor && !m.onQuickPanel {
			cursor = "▶ "
			style = selectedStyle
		}
		icon := "📁"
		if !entry.IsDir {
			icon = "📄"
		}
		dirLines = append(dirLines, fmt.Sprintf("%s%s %s", cursor, icon, style.Render(entry.Name)))
	}

	var dirContent string
	if m.err != nil {
		dirContent = errorStyle.Render("Error: " + m.err.Error())
	} else if len(m.entries) == 0 {
		if m.mode == "file" {
			dirContent = dimStyle.Render("  (no files)")
		} else {
			dirContent = dimStyle.Render("  (empty directory)")
		}
	} else {
		// Limit visible lines
		maxLines := popupH - 6
		if maxLines < 5 {
			maxLines = 5
		}
		start := 0
		if len(dirLines) > maxLines {
			start = m.cursor - maxLines/2
			if start < 0 {
				start = 0
			}
			end := start + maxLines
			if end > len(dirLines) {
				end = len(dirLines)
			}
			dirLines = dirLines[start:end]
		}
		dirContent = strings.Join(dirLines, "\n")
	}
	dirPanel := dirListStyle.Width(rightW).Render(titleStyle.Render("Folders") + "\n" + dirContent)

	// Breadcrumb
	breadcrumb := m.buildBreadcrumb()

	// Current path
	currentPath := pathStyle.Render("Current: " + m.currentPath)

	// Help
	var help string
	if m.mode == "file" {
		help = helpStyle.Render("[↑/↓] Move  [→] Open dir  [←] Parent  [Tab] Switch panel  [Enter] Select file  [Esc] Cancel")
	} else {
		help = helpStyle.Render("[↑/↓] Move  [→] Open  [←] Parent  [Tab] Switch panel  [Enter] Select folder  [Esc] Cancel")
	}

	// Combine content
	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(m.Title),
		lipgloss.JoinHorizontal(lipgloss.Top, qaPanel, dirPanel),
		breadcrumbStyle.Render(breadcrumb),
		currentPath,
		"",
		help,
	)

	// Center the popup
	popup := popupStyle.Width(popupW).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, popup)
}

func (m Model) buildBreadcrumb() string {
	home := homeDir()
	path := m.currentPath

	if path == home {
		return "Home"
	}
	if path == "/" {
		return "/"
	}

	// Build breadcrumb from path
	rel, err := filepath.Rel(home, path)
	if err != nil {
		return path
	}

	if rel == "." {
		return "Home"
	}

	parts := strings.Split(rel, string(os.PathSeparator))
	result := "Home"
	for _, part := range parts {
		result += " > " + part
	}
	return result
}
