package directorypicker

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return CancelledMsg{} }

		case "enter":
			if m.onQuickPanel {
				// Navigate into quick access path
				m.currentPath = m.quickAccess[m.quickCursor].Path
				m.loadDir()
				m.onQuickPanel = false
				return m, nil
			}
			// In file mode, enter on a file selects it
			if m.mode == "file" && len(m.entries) > 0 {
				selected := m.entries[m.cursor]
				if !selected.IsDir {
					return m, func() tea.Msg { return SelectedMsg{Path: selected.Path} }
				}
			}
			// In dir mode, enter selects the current directory
			if m.mode == "dir" {
				return m, func() tea.Msg { return SelectedMsg{Path: m.currentPath} }
			}
			// In file mode, enter on a directory navigates into it
			if m.mode == "file" && len(m.entries) > 0 {
				selected := m.entries[m.cursor]
				if selected.IsDir {
					m.currentPath = selected.Path
					m.loadDir()
				}
			}

		case "tab":
			m.onQuickPanel = !m.onQuickPanel
			return m, nil

		case "up", "k":
			if m.onQuickPanel {
				if m.quickCursor > 0 {
					m.quickCursor--
				}
			} else {
				if m.cursor > 0 {
					m.cursor--
				}
			}

		case "down", "j":
			if m.onQuickPanel {
				if m.quickCursor < len(m.quickAccess)-1 {
					m.quickCursor++
				}
			} else {
				if m.cursor < len(m.entries)-1 {
					m.cursor++
				}
			}

		case "right", "l":
			if !m.onQuickPanel && len(m.entries) > 0 {
				selected := m.entries[m.cursor]
				if selected.IsDir {
					m.currentPath = selected.Path
					m.loadDir()
				}
			}

		case "left", "h":
			if !m.onQuickPanel {
				m.currentPath = parentPath(m.currentPath)
				m.loadDir()
			}

		case "home":
			if m.onQuickPanel {
				m.quickCursor = 0
			} else {
				m.cursor = 0
			}

		case "end":
			if m.onQuickPanel {
				m.quickCursor = len(m.quickAccess) - 1
			} else {
				m.cursor = len(m.entries) - 1
			}

		case "pgup":
			if !m.onQuickPanel {
				m.cursor -= 10
				if m.cursor < 0 {
					m.cursor = 0
				}
			}

		case "pgdown":
			if !m.onQuickPanel {
				m.cursor += 10
				if m.cursor >= len(m.entries) {
					m.cursor = len(m.entries) - 1
				}
			}
		}

		// If on quick panel and user navigates, enter the quick access path
		if m.onQuickPanel {
			switch msg.String() {
			case "right", "l":
				m.currentPath = m.quickAccess[m.quickCursor].Path
				m.loadDir()
				m.onQuickPanel = false
				return m, nil
			}
		}
	}

	return m, nil
}
